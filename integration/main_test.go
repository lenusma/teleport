/*
Copyright 2022 Gravitational, Inc.

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
package integration

import (
	"os"
	"testing"
	"time"

	"github.com/gravitational/teleport/integration/helpers"
	"github.com/gravitational/teleport/lib/srv"
	"github.com/gravitational/teleport/lib/utils"
)

// TestMain will re-execute Teleport to run a command if "exec" is passed to
// it as an argument. Otherwise, it will run tests as normal.
func TestMain(m *testing.M) {
	utils.InitLoggerForTests()
	helpers.SetTestTimeouts(100 * time.Millisecond)
	// If the test is re-executing itself, execute the command that comes over
	// the pipe.
	if srv.IsReexec() {
		srv.RunAndExit(os.Args[1])
		return
	}

	// Otherwise run tests as normal.
	code := m.Run()
	os.Exit(code)
}
