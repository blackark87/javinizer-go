package config

import (
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// GetOperationMode parses mode into an OperationMode, defaulting to organize on empty or invalid input.
func GetOperationMode(mode string) operationmode.OperationMode {
	if mode == "" {
		return operationmode.OperationModeOrganize
	}
	parsed, err := operationmode.ParseOperationMode(mode)
	if err != nil {
		return operationmode.OperationModeOrganize
	}
	return parsed
}

// GetOperationMode resolves the configured output operation mode, defaulting on empty or invalid input.
func (o *OutputOperationConfig) GetOperationMode() operationmode.OperationMode {
	return GetOperationMode(string(o.OperationMode))
}

// GetOperationMode delegates to OutputOperationConfig for backward compatibility
// with callers that reference OutputConfig.GetOperationMode().
func (o *OutputConfig) GetOperationMode() operationmode.OperationMode {
	return o.Operation.GetOperationMode()
}
