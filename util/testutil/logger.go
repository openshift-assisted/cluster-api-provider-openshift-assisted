/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package testutil

import (
	"io"
	"os"
	"strconv"

	uberzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// SetupTestLogger configures the test logger with the specified writer.
// The log level can be controlled via the TEST_LOGLEVEL environment variable.
// If TEST_LOGLEVEL is not set or invalid, it defaults to -3.
//
// Example usage in BeforeSuite:
//
//	testutil.SetupTestLogger(GinkgoWriter)
//
// To set a custom log level when running tests:
//
//	TEST_LOGLEVEL=-9 go test ./...
func SetupTestLogger(writer io.Writer) {
	SetupTestLoggerWithDefault(writer, -3)
}

// SetupTestLoggerWithDefault configures the test logger with the specified writer and default log level.
// The log level can be controlled via the TEST_LOGLEVEL environment variable.
// If TEST_LOGLEVEL is not set or invalid, it uses the provided defaultLevel.
//
// Example usage in BeforeSuite:
//
//	testutil.SetupTestLoggerWithDefault(GinkgoWriter, -9)
func SetupTestLoggerWithDefault(writer io.Writer, defaultLevel int) {
	logLevel := defaultLevel
	if envLevel := os.Getenv("TEST_LOGLEVEL"); envLevel != "" {
		if parsedLevel, err := strconv.Atoi(envLevel); err == nil {
			logLevel = parsedLevel
		}
	}

	lvl := uberzap.NewAtomicLevelAt(zapcore.Level(logLevel))
	logf.SetLogger(zap.New(
		zap.WriteTo(writer),
		zap.UseDevMode(true),
		zap.Level(&lvl),
	))
}
