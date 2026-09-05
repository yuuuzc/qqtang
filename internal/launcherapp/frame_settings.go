package launcherapp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

var supportedClientFrameRates = map[int]struct{}{
	45: {}, 60: {}, 144: {}, 300: {},
}

func applyClientFrameSettings(clientRoot string, maximumFPS int, showFPS bool) error {
	if _, ok := supportedClientFrameRates[maximumFPS]; !ok {
		return fmt.Errorf("客户端最大帧率 %d 不受支持（可选 45、60、144、300）", maximumFPS)
	}
	files := []struct {
		path  string
		key   string
		value int
	}{
		{path: filepath.Join(clientRoot, "config", "GameCFG.ini"), key: "limitfps", value: maximumFPS},
		{path: filepath.Join(clientRoot, "devConfig.txt"), key: "fps", value: boolInteger(showFPS)},
	}
	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			return fmt.Errorf("读取客户端帧率配置 %s：%w", file.path, err)
		}
		updated, err := replaceScalarSetting(data, file.key, file.value)
		if err != nil {
			return fmt.Errorf("更新客户端帧率配置 %s：%w", file.path, err)
		}
		if bytes.Equal(data, updated) {
			continue
		}
		if err := os.WriteFile(file.path, updated, 0o644); err != nil {
			return fmt.Errorf("写入客户端帧率配置 %s：%w", file.path, err)
		}
	}
	return nil
}

func replaceScalarSetting(data []byte, key string, value int) ([]byte, error) {
	expression := regexp.MustCompile(`(?mi)^([ \t]*` + regexp.QuoteMeta(key) + `[ \t]*=[ \t]*)[^\r\n]*`)
	matches := expression.FindAllSubmatchIndex(data, -1)
	if len(matches) != 1 {
		return nil, fmt.Errorf("配置项 %s 出现 %d 次，要求恰好一次", key, len(matches))
	}
	match := matches[0]
	updated := make([]byte, 0, len(data)+8)
	updated = append(updated, data[:match[0]]...)
	updated = append(updated, data[match[2]:match[3]]...)
	updated = strconv.AppendInt(updated, int64(value), 10)
	updated = append(updated, data[match[1]:]...)
	return updated, nil
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
