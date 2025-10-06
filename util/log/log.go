package log

// Log levels for controller-runtime's structured logging.
// Higher numeric values indicate more verbose/detailed logging.
// The logger will only output messages at or below the configured log level.
const (
	// WarningLevel - Lowest verbosity, only warnings and errors
	WarningLevel = 1
	// InfoLevel - Standard informational messages
	InfoLevel = 3
	// DebugLevel - Debug information, more verbose than info
	DebugLevel = 6
	// TraceLevel - Highest verbosity, detailed tracing information
	TraceLevel = 9
)
