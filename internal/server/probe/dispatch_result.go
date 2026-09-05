package probe

// protocolMessageResult is the common boundary between protocol-family
// handlers and the TCP delivery loop.
type protocolMessageResult struct {
	handled        bool
	beforeResponse []byte
	beforeResult   string
	response       []byte
	result         string
	followUp       []byte
	followUpResult string
	postResponse   func()
}
