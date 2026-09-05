package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"qqtang/internal/accountauth"
	"qqtang/internal/tooling/winlaunch"
)

type options struct {
	pid             uint32
	tracePath       string
	authAddress     string
	attachTimeout   time.Duration
	clickTimeout    time.Duration
	postLoadDelay   time.Duration
	deliveryTimeout time.Duration
	lobbyTimeout    time.Duration
	duration        time.Duration
	keepUntilExit   bool
	outputPath      string
	readyPath       string
	fixShopOrdering bool
	skipLobbyStage  bool
}

type immediateLoginResult struct {
	PID              uint32                                   `json:"pid"`
	Stage            string                                   `json:"stage"`
	StartedUTC       string                                   `json:"started_utc"`
	UpdatedUTC       string                                   `json:"updated_utc"`
	SSOModuleLoaded  bool                                     `json:"sso_module_loaded"`
	ReplayAttempts   int                                      `json:"replay_attempts"`
	Replay           winlaunch.SSOMessageReplayResult         `json:"replay"`
	RejectAttempts   int                                      `json:"reject_replay_attempts"`
	RejectReplay     winlaunch.SSOMessageReplayResult         `json:"reject_replay"`
	RejectCallback   winlaunch.LoginCallbackObservation       `json:"reject_callback"`
	RejectError      string                                   `json:"reject_delivery_error,omitempty"`
	Callback         winlaunch.LoginCallbackOverrideCapture   `json:"callback"`
	TimeoutSuppress  winlaunch.LoginTimeoutSuppressionCapture `json:"timeout_suppression"`
	LobbyUnlock      winlaunch.LobbyStageWriteCapture         `json:"lobby_unlock"`
	LobbyMaintenance winlaunch.LobbyStageMaintenanceCapture   `json:"lobby_maintenance"`
	LobbyError       string                                   `json:"lobby_preparation_error,omitempty"`
	ShopOrdering     winlaunch.ShopConnectCompatibilityResult `json:"shop_ordering"`
	ShopError        string                                   `json:"shop_ordering_error,omitempty"`
	AuthenticatedUIN uint32                                   `json:"authenticated_uin,omitempty"`
	Authenticated    bool                                     `json:"authenticated"`
	AuthAttempts     int                                      `json:"auth_attempts"`
	LastAuthError    string                                   `json:"last_auth_error,omitempty"`
	Error            string                                   `json:"error,omitempty"`
}

func main() {
	pid := flag.Uint("pid", 0, "target Client.exe process ID")
	tracePath := flag.String("trace", "", "verified SSO trace containing one complete failed 0x1980 payload")
	authAddress := flag.String("auth-address", "", "local QQTang game server address used for account password challenge-response")
	attachTimeout := flag.Duration("attach-timeout", 30*time.Second, "maximum time to install the two narrow compatibility patches")
	clickTimeout := flag.Duration("click-timeout", 24*time.Hour, "maximum time to wait for the Login click and lazy SSOPlatform.dll load")
	postLoadDelay := flag.Duration("post-load-delay", 250*time.Millisecond, "settling time after the lazy SSO module loads")
	deliveryTimeout := flag.Duration("delivery-timeout", 10*time.Second, "maximum time for one GUI-thread WM_COPYDATA delivery")
	lobbyTimeout := flag.Duration("lobby-timeout", 24*time.Hour, "maximum time to wait for the active local lobby")
	duration := flag.Duration("duration", 24*time.Hour, "maximum time to keep stale-callback protection installed")
	keepUntilExit := flag.Bool("keep-until-client-exit", false, "keep compatibility patches installed until Client.exe exits")
	outputPath := flag.String("out", "", "JSON result output path")
	readyPath := flag.String("ready", "", "optional JSON readiness marker path")
	fixShopOrdering := flag.Bool("fix-local-shop-ordering", false, "diagnostic opt-in: rewrite localhost shop socket callback ordering")
	skipLobbyStage := flag.Bool("skip-lobby-stage", false, "diagnostic only: do not advance or maintain the legacy lobby stage")
	flag.Parse()

	if *pid == 0 || *tracePath == "" || *authAddress == "" || *outputPath == "" {
		fmt.Fprintln(os.Stderr, "qqt-login-immediate: -pid, -trace, -auth-address, and -out are required")
		os.Exit(2)
	}
	result, runErr := run(options{
		pid: uint32(*pid), tracePath: *tracePath, authAddress: *authAddress,
		attachTimeout: *attachTimeout, clickTimeout: *clickTimeout,
		postLoadDelay: *postLoadDelay, deliveryTimeout: *deliveryTimeout,
		lobbyTimeout: *lobbyTimeout,
		duration:     *duration, keepUntilExit: *keepUntilExit, outputPath: *outputPath, readyPath: *readyPath,
		fixShopOrdering: *fixShopOrdering,
		skipLobbyStage:  *skipLobbyStage,
	})
	if err := writeJSON(*outputPath, result); err != nil {
		fmt.Fprintln(os.Stderr, "qqt-login-immediate:", err)
		os.Exit(1)
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "qqt-login-immediate:", runErr)
		os.Exit(1)
	}
}

func run(config options) (result immediateLoginResult, runErr error) {
	started := time.Now().UTC()
	result = immediateLoginResult{PID: config.pid, Stage: "initializing", StartedUTC: started.Format(time.RFC3339Nano)}
	type shopInstallResult struct {
		result winlaunch.ShopConnectCompatibilityResult
		err    error
	}
	var shopInstall chan shopInstallResult
	if config.fixShopOrdering {
		shopInstall = make(chan shopInstallResult, 1)
		go func() {
			installed, installErr := winlaunch.InstallShopConnectCompatibility(config.pid, 0, 45*time.Second)
			shopInstall <- shopInstallResult{result: installed, err: installErr}
		}()
	}
	fail := func(err error) (immediateLoginResult, error) {
		result.Stage = "failed"
		result.UpdatedUTC = time.Now().UTC().Format(time.RFC3339Nano)
		result.Error = err.Error()
		if config.readyPath != "" {
			_ = writeJSON(config.readyPath, result)
		}
		return result, err
	}

	copyDataID, failurePayload, err := loadFailurePayload(config.tracePath)
	if err != nil {
		return fail(err)
	}
	credentialCapture, err := winlaunch.InstallLoginCredentialCapture(config.pid, config.attachTimeout)
	if err != nil {
		return fail(fmt.Errorf("install local credential capture: %w", err))
	}
	defer credentialCapture.Close()

	result.Stage = "armed"
	result.UpdatedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	if config.readyPath != "" {
		if err := writeJSON(config.readyPath, result); err != nil {
			return fail(fmt.Errorf("write armed marker: %w", err))
		}
	}
	if err := winlaunch.WaitForProcessModule(config.pid, "SSOPlatform.dll", config.clickTimeout); err != nil {
		return fail(fmt.Errorf("wait for Login click: %w", err))
	}
	result.SSOModuleLoaded = true
	if config.postLoadDelay > 0 {
		time.Sleep(config.postLoadDelay)
	}
	suppressor, err := winlaunch.InstallLoginTimeoutSuppressor(config.pid, config.attachTimeout)
	if err != nil {
		return fail(fmt.Errorf("install timeout suppression: %w", err))
	}
	defer suppressor.Close()
	observer, err := winlaunch.InstallLoginCallbackObserver(config.pid, config.attachTimeout)
	if err != nil {
		return fail(fmt.Errorf("install failed-login callback observer: %w", err))
	}
	defer observer.Close()
	authDeadline := time.Now().Add(config.clickTimeout)
	var authenticatedProfile accountauth.Message
	for !result.Authenticated {
		remaining := time.Until(authDeadline)
		if remaining <= 0 {
			return fail(fmt.Errorf("local account authentication timed out after %d attempts", result.AuthAttempts))
		}
		credential, captureErr := credentialCapture.TakeUntilFirst(remaining)
		if captureErr != nil {
			return fail(fmt.Errorf("capture local login password: %w", captureErr))
		}
		result.AuthAttempts++
		result.AuthenticatedUIN = credential.UIN
		var authErr error
		authenticatedProfile, authErr = authenticateLocalAccount(config.authAddress, credential)
		clear(credential.Password)
		if authErr == nil {
			result.Authenticated = true
			result.LastAuthError = ""
			result.Stage = "auth_accepted"
			result.UpdatedUTC = time.Now().UTC().Format(time.RFC3339Nano)
			if config.readyPath != "" {
				if err := writeJSON(config.readyPath, result); err != nil {
					return fail(fmt.Errorf("write authentication-accepted marker: %w", err))
				}
			}
			break
		}
		result.Stage = "auth_rejected"
		result.LastAuthError = authErr.Error()
		result.UpdatedUTC = time.Now().UTC().Format(time.RFC3339Nano)
		if config.readyPath != "" {
			_ = writeJSON(config.readyPath, result)
		}
		// A successful SendMessageTimeout call only proves that Windows delivered
		// WM_COPYDATA. Retry until the verified Client callback itself runs. The
		// stable payload produces legacy SSO status 4 (retry another SSO endpoint),
		// so arm a narrow translation to the Client's native password-error status
		// 2 only after the local Go account service has rejected the password.
		if err := observer.ArmPasswordRejection(); err != nil {
			return fail(err)
		}
		previousCalls := observer.Calls()
		result.RejectError = ""
		for attempt := 1; attempt <= 6; attempt++ {
			result.RejectAttempts = attempt
			result.RejectReplay, err = winlaunch.ReplaySSOFailureMessage(config.pid, copyDataID, failurePayload, config.deliveryTimeout)
			if err != nil {
				result.RejectError = err.Error()
				continue
			}
			result.RejectCallback = observer.CaptureAfter(previousCalls, 600*time.Millisecond)
			if result.RejectCallback.Calls > previousCalls {
				break
			}
			result.RejectError = "WM_COPYDATA was delivered but the Client login callback did not run"
		}
		observer.DisarmPasswordRejection()
		result.UpdatedUTC = time.Now().UTC().Format(time.RFC3339Nano)
		if config.readyPath != "" {
			_ = writeJSON(config.readyPath, result)
		}
		if result.RejectCallback.Calls <= previousCalls {
			return fail(fmt.Errorf("deliver native password rejection after %d attempts: %s", result.RejectAttempts, result.RejectError))
		}
		if result.RejectCallback.OriginalStatus != 4 {
			return fail(fmt.Errorf("native password rejection returned unexpected Client status %d", result.RejectCallback.OriginalStatus))
		}
		if result.RejectCallback.DeliveredStatus != 2 {
			return fail(fmt.Errorf("native password rejection delivered unexpected Client status %d", result.RejectCallback.DeliveredStatus))
		}
	}
	if authenticatedProfile.Nickname == "" {
		return fail(errors.New("server accepted the account without an authoritative character profile"))
	}
	localPayload, err := winlaunch.BuildLocalLoginCallbackPayloadWithGender(authenticatedProfile.Nickname, authenticatedProfile.Gender)
	if err != nil {
		return fail(fmt.Errorf("build authenticated login profile: %w", err))
	}
	observer.Close()
	override, err := winlaunch.InstallLoginCallbackOverride(config.pid, config.attachTimeout, localPayload)
	if err != nil {
		return fail(fmt.Errorf("install local callback override: %w", err))
	}
	defer override.Close()
	var lastDeliveryErr error
	for attempt := 1; attempt <= 6; attempt++ {
		result.ReplayAttempts = attempt
		result.Replay, lastDeliveryErr = winlaunch.ReplaySSOFailureMessage(config.pid, copyDataID, failurePayload, config.deliveryTimeout)
		if lastDeliveryErr == nil {
			result.Callback = override.CaptureUntilFirst(600 * time.Millisecond)
			if result.Callback.Calls != 0 {
				break
			}
		}
	}
	if result.Callback.Calls == 0 {
		if lastDeliveryErr == nil {
			lastDeliveryErr = errors.New("the client accepted WM_COPYDATA but did not enter its verified login callback")
		}
		return fail(fmt.Errorf("trigger immediate local login after %d attempts: %w", result.ReplayAttempts, lastDeliveryErr))
	}
	result.Stage = "login_replayed"
	result.UpdatedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	if config.readyPath != "" {
		if err := writeJSON(config.readyPath, result); err != nil {
			return fail(fmt.Errorf("write login-replayed marker: %w", err))
		}
	}

	// A tutorial-completed local profile reaches the lobby through only one
	// legacy entry pass. Complete that one proven stage transition. Inventory
	// is supplied by RESPONSE_LOGIN from SQLite; the startup helper must not
	// inject a special-case adventure card into client memory.
	if config.skipLobbyStage {
		result.Stage = "lobby_prepare_skipped"
	} else {
		result.LobbyUnlock, err = winlaunch.WaitAndAdvanceTutorialCompletedLobbyStage(config.pid, config.lobbyTimeout)
		if err != nil {
			result.Stage = "lobby_prepare_failed"
			result.LobbyError = err.Error()
		} else {
			result.Stage = "lobby_prepared"
		}
	}
	if shopInstall != nil {
		installed := <-shopInstall
		result.ShopOrdering = installed.result
		if installed.err != nil {
			result.ShopError = installed.err.Error()
		}
	}
	result.UpdatedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	if config.readyPath != "" {
		if markerErr := writeJSON(config.readyPath, result); markerErr != nil {
			return fail(fmt.Errorf("write lobby-preparation marker: %w", markerErr))
		}
	}
	// Keep both patches installed while the client lives. Old SSO can emit a
	// second terminal callback after the first one, so restoring here would
	// reintroduce the stale timeout/error dialog.
	maintenanceContext, stopMaintenance := context.WithCancel(context.Background())
	var maintenanceResult chan winlaunch.LobbyStageMaintenanceCapture
	if !config.skipLobbyStage {
		maintenanceResult = make(chan winlaunch.LobbyStageMaintenanceCapture, 1)
		go func() {
			capture, maintenanceErr := winlaunch.MaintainTutorialCompletedLobbyStage(maintenanceContext, config.pid, result.AuthenticatedUIN)
			if maintenanceErr != nil {
				capture.LastReadError = maintenanceErr.Error()
			}
			maintenanceResult <- capture
		}()
	}
	if config.keepUntilExit {
		result.Callback = override.CaptureUntilClientExit()
	} else {
		result.Callback = override.Capture(config.duration)
	}
	stopMaintenance()
	if maintenanceResult != nil {
		result.LobbyMaintenance = <-maintenanceResult
	}
	result.TimeoutSuppress = suppressor.Capture(0)
	result.Stage = "finished"
	result.UpdatedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	return result, nil
}

func authenticateLocalAccount(address string, credential winlaunch.LocalLoginCredential) (accountauth.Message, error) {
	if credential.UIN == 0 || len(credential.Password) == 0 {
		return accountauth.Message{}, fmt.Errorf("captured credential is empty")
	}
	connection, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return accountauth.Message{}, fmt.Errorf("connect %s: %w", address, err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(15 * time.Second))
	hello, err := accountauth.Marshal(accountauth.Message{Type: accountauth.MessageHello, UIN: credential.UIN})
	if err != nil {
		return accountauth.Message{}, err
	}
	if _, err := connection.Write(hello); err != nil {
		return accountauth.Message{}, fmt.Errorf("send password challenge request: %w", err)
	}
	challenge, err := readAuthMessage(connection)
	if err != nil {
		return accountauth.Message{}, err
	}
	if challenge.Type == accountauth.MessageResult {
		return accountauth.Message{}, fmt.Errorf("%s", challenge.Reason)
	}
	if challenge.Type != accountauth.MessageChallenge || challenge.UIN != credential.UIN {
		return accountauth.Message{}, fmt.Errorf("server returned an invalid password challenge")
	}
	if challenge.Iterations < 100_000 || challenge.Iterations > 2_000_000 || len(challenge.Salt) < accountauth.SaltSize || len(challenge.Salt) > 64 || len(challenge.Nonce) != accountauth.NonceSize {
		return accountauth.Message{}, fmt.Errorf("server returned unsafe password challenge parameters")
	}
	proofBytes, err := accountauth.ClientProof(credential.Password, challenge.Salt, challenge.Iterations, challenge.Nonce, credential.UIN)
	if err != nil {
		return accountauth.Message{}, err
	}
	proof, err := accountauth.Marshal(accountauth.Message{Type: accountauth.MessageProof, UIN: credential.UIN, Proof: proofBytes})
	clear(proofBytes)
	if err != nil {
		return accountauth.Message{}, err
	}
	if _, err := connection.Write(proof); err != nil {
		return accountauth.Message{}, fmt.Errorf("send password proof: %w", err)
	}
	result, err := readAuthMessage(connection)
	if err != nil {
		return accountauth.Message{}, err
	}
	if result.Type != accountauth.MessageResult {
		return accountauth.Message{}, fmt.Errorf("server returned an invalid password result")
	}
	if !result.Accepted {
		if result.Reason == "" {
			result.Reason = "账号或密码错误"
		}
		return accountauth.Message{}, errors.New(result.Reason)
	}
	if result.Nickname == "" || result.Gender > 1 {
		return accountauth.Message{}, errors.New("server returned an invalid account profile")
	}
	return result, nil
}

func readAuthMessage(reader io.Reader) (accountauth.Message, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return accountauth.Message{}, fmt.Errorf("read password response header: %w", err)
	}
	length := int(binary.BigEndian.Uint32(header))
	if length < 13 || length > 1024 {
		return accountauth.Message{}, fmt.Errorf("password response length %d is invalid", length)
	}
	frame := make([]byte, length)
	copy(frame, header)
	if _, err := io.ReadFull(reader, frame[4:]); err != nil {
		return accountauth.Message{}, fmt.Errorf("read password response: %w", err)
	}
	return accountauth.Unmarshal(frame)
}

func loadFailurePayload(path string) (uint32, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, nil, fmt.Errorf("read SSO trace: %w", err)
	}
	var capture winlaunch.SSOMessageCapture
	if err := json.Unmarshal(data, &capture); err != nil {
		return 0, nil, fmt.Errorf("decode SSO trace: %w", err)
	}
	var selected *winlaunch.SSOMessageCall
	for index := range capture.Calls {
		call := &capture.Calls[index]
		if call.CopyDataID == 0x1980 && call.DataLength != 0 && call.DataLength == call.CapturedLength && call.PayloadHex != "" {
			selected = call
		}
	}
	if selected == nil {
		return 0, nil, errors.New("SSO trace contains no complete 0x1980 payload")
	}
	payload, err := hex.DecodeString(selected.PayloadHex)
	if err != nil || len(payload) != int(selected.DataLength) {
		return 0, nil, errors.New("selected 0x1980 payload is invalid or incomplete")
	}
	return selected.CopyDataID, payload, nil
}

func writeJSON(path string, value any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(value)
	closeErr := file.Close()
	if encodeErr != nil {
		return fmt.Errorf("encode %s: %w", path, encodeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	return nil
}
