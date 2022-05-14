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

package db

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"

	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/lib/defaults"
	libevents "github.com/gravitational/teleport/lib/events"
	"github.com/gravitational/teleport/lib/srv/alpnproxy"
	"github.com/gravitational/teleport/lib/srv/db/common"
	"github.com/gravitational/teleport/lib/srv/db/snowflake"
	"github.com/stretchr/testify/require"
)

func init() {
	// Override Snowflake engine that is used normally with the test one
	// with custom HTTP client.
	common.RegisterEngine(newTestSnowflakeEngine, defaults.ProtocolSnowflake)
}

func newTestSnowflakeEngine(ec common.EngineConfig) common.Engine {
	return &snowflake.Engine{
		EngineConfig: ec,
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
				// Test Snowflake mock instance listens on localhost, but Snowflake's test uses localhost.snowflakecomputing.com
				// as the Snowflake URL. Here we map the fake URL to localhost, so tests connect to our mock not
				// the real Snowflake instance.
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					_, port, err := net.SplitHostPort(addr)
					if err != nil {
						return nil, err
					}
					return (&net.Dialer{}).DialContext(ctx, network, "localhost:"+port)
				},
			},
		},
	}
}

func TestAccessSnowflake(t *testing.T) {
	ctx := context.Background()
	testCtx := setupTestContext(ctx, t, withSnowflake("snowflake"))
	go testCtx.startHandlingConnections()

	tests := []struct {
		desc         string
		user         string
		role         string
		allowDbNames []string
		allowDbUsers []string
		dbName       string
		dbUser       string
		err          string
	}{
		{
			desc:         "has access to all database names and users",
			user:         "alice",
			role:         "admin",
			allowDbNames: []string{types.Wildcard},
			allowDbUsers: []string{types.Wildcard},
			dbName:       "snowflake",
			dbUser:       "snowflake",
		},
		{
			desc:         "has access to nothing",
			user:         "alice",
			role:         "admin",
			allowDbNames: []string{},
			allowDbUsers: []string{},
			dbName:       "snowflake",
			dbUser:       "snowflake",
			err:          "HTTP: 401",
		},
		{
			desc:         "no access to databases",
			user:         "alice",
			role:         "admin",
			allowDbNames: []string{},
			allowDbUsers: []string{types.Wildcard},
			dbName:       "snowflake",
			dbUser:       "snowflake",
			err:          "HTTP: 401",
		},
		{
			desc:         "no access to users",
			user:         "alice",
			role:         "admin",
			allowDbNames: []string{types.Wildcard},
			allowDbUsers: []string{},
			dbName:       "snowflake",
			dbUser:       "snowflake",
			err:          "HTTP: 401",
		},
		{
			desc:         "access allowed to specific user/database",
			user:         "alice",
			role:         "admin",
			allowDbNames: []string{"metrics"},
			allowDbUsers: []string{"alice"},
			dbName:       "metrics",
			dbUser:       "alice",
		},
		{
			desc:         "access denied to specific user/database",
			user:         "alice",
			role:         "admin",
			allowDbNames: []string{"metrics"},
			allowDbUsers: []string{"alice"},
			dbName:       "snowflake",
			dbUser:       "snowflake",
			err:          "HTTP: 401",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			// Create user/role with the requested permissions.
			testCtx.createUserAndRole(ctx, t, test.user, test.role, test.allowDbUsers, test.allowDbNames)

			// Try to connect to the database as this user.
			dbConn, proxy, err := testCtx.snowflakeClient(ctx, test.user, "snowflake", test.dbUser, test.dbName)

			t.Cleanup(func() {
				proxy.Close()
			})

			require.NoError(t, err)

			// Execute a query.
			result, err := dbConn.QueryContext(ctx, "select 42")
			if test.err != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), test.err)
				return
			}
			require.NoError(t, err)
			defer result.Close()

			for result.Next() {
				var res int
				err = result.Scan(&res)
				require.NoError(t, err)
				require.Equal(t, 42, res)
			}

			// Disconnect.
			err = dbConn.Close()
			require.NoError(t, err)
		})
	}
}

func TestAuditSnowflake(t *testing.T) {
	ctx := context.Background()
	testCtx := setupTestContext(ctx, t, withSnowflake("snowflake"))
	go testCtx.startHandlingConnections()

	testCtx.createUserAndRole(ctx, t, "alice", "admin", []string{"admin"}, []string{types.Wildcard})

	t.Run("access denied", func(t *testing.T) {
		// Access denied should trigger an unsuccessful session start event.
		dbConn, proxy, err := testCtx.snowflakeClient(ctx, "alice", "snowflake", "notadmin", "")
		require.NoError(t, err)
		err = dbConn.PingContext(ctx)
		require.Error(t, err)
		waitForEvent(t, testCtx, libevents.DatabaseSessionStartFailureCode)
		proxy.Close()
	})

	var dbConn *sql.DB
	var proxy *alpnproxy.LocalProxy
	t.Cleanup(func() {
		if proxy != nil {
			proxy.Close()
		}
	})

	t.Run("session starts event", func(t *testing.T) {
		// Connect should trigger successful session start event.
		var err error

		dbConn, proxy, err = testCtx.snowflakeClient(ctx, "alice", "snowflake", "admin", "")
		require.NoError(t, err)
		err = dbConn.PingContext(ctx)
		require.NoError(t, err)
		waitForEvent(t, testCtx, libevents.DatabaseSessionStartCode)
	})

	t.Run("command sends", func(t *testing.T) {
		// SET should trigger Query event.
		result, err := dbConn.QueryContext(ctx, "select 42")
		require.NoError(t, err)
		defer result.Close()

		for result.Next() {
			var res int
			err = result.Scan(&res)
			require.NoError(t, err)
			require.Equal(t, 42, res)
		}

		waitForEvent(t, testCtx, libevents.DatabaseSessionQueryCode)
	})

	t.Run("session ends event", func(t *testing.T) {
		t.Skip() //TODO(jakule): Driver for some reason doesn't terminate the session.
		// Closing connection should trigger session end event.
		err := dbConn.Close()
		require.NoError(t, err)
		waitForEvent(t, testCtx, libevents.DatabaseSessionEndCode)
	})
}

func TestTokenRefresh(t *testing.T) {
	ctx := context.Background()
	testCtx := setupTestContext(ctx, t, withSnowflake("snowflake", snowflake.TestForceTokenRefresh()))
	go testCtx.startHandlingConnections()

	testCtx.createUserAndRole(ctx, t, "alice", "admin", []string{"admin"}, []string{types.Wildcard})

	dbConn, proxy, err := testCtx.snowflakeClient(ctx, "alice", "snowflake", "admin", "")
	require.NoError(t, err)
	t.Cleanup(func() {
		proxy.Close()
	})

	err = dbConn.PingContext(ctx)
	require.NoError(t, err)

	result, err := dbConn.QueryContext(ctx, "select 42")
	require.NoError(t, err)
	defer result.Close()

	for result.Next() {
		var res int
		err = result.Scan(&res)
		require.NoError(t, err)
		require.Equal(t, 42, res)
	}
}

func TestTokenSession(t *testing.T) {
	ctx := context.Background()
	testCtx := setupTestContext(ctx, t, withSnowflake("snowflake"))
	go testCtx.startHandlingConnections()

	testCtx.createUserAndRole(ctx, t, "alice", "admin", []string{"admin"}, []string{types.Wildcard})

	dbConn, proxy, err := testCtx.snowflakeClient(ctx, "alice", "snowflake", "admin", "")
	require.NoError(t, err)
	t.Cleanup(func() {
		proxy.Close()
	})

	err = dbConn.PingContext(ctx)
	require.NoError(t, err)

	result, err := dbConn.QueryContext(ctx, "select 42")
	require.NoError(t, err)
	defer result.Close()

	for result.Next() {
		var res int
		err = result.Scan(&res)
		require.NoError(t, err)
		require.Equal(t, 42, res)
	}

	require.NoError(t, err)

	appSessions, err := testCtx.authServer.GetAppSessions(ctx)
	require.NoError(t, err)
	require.Len(t, appSessions, 2)

	snowflakeSess := appSessions[1] //TODO(jakule): fix me. Order is random.
	require.Equal(t, types.KindSnowflakeSession, snowflakeSess.GetSubKind())

	const queryBody = `{
  "sqlText": "select 42"
}
`
	mockProxyQueryURL := "http://" + proxy.GetAddr() + "/queries/v1/query-request"

	queryReq, err := http.NewRequestWithContext(ctx, "POST", mockProxyQueryURL, bytes.NewReader([]byte(queryBody)))
	require.NoError(t, err)

	queryReq.Header.Set("Authorization", fmt.Sprintf("Snowflake Token=\"Teleport:%s\"", snowflakeSess.GetName()))
	queryReq.Header.Set("Content-Type", "application/json")
	queryReq.Header.Set("Content-Length", strconv.Itoa(len(queryBody)))

	resp, err := (&http.Client{}).Do(queryReq)
	require.NoError(t, err)

	require.Equal(t, 200, resp.StatusCode)
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	jsonMap := struct {
		Data struct {
			Rowset [][]string `json:"rowset"`
		} `json:"data"`
	}{}
	err = json.Unmarshal(respBody, &jsonMap)
	require.NoError(t, err)

	require.Equal(t, jsonMap.Data.Rowset[0][0], "42")
}

func withSnowflake(name string, opts ...snowflake.TestServerOption) withDatabaseOption {
	return func(t *testing.T, ctx context.Context, testCtx *testContext) types.Database {
		snowflakeServer, err := snowflake.NewTestServer(common.TestServerConfig{
			Name:       name,
			AuthClient: testCtx.authClient,
			ClientAuth: tls.RequireAndVerifyClientCert,
		}, opts...)
		require.NoError(t, err)
		go snowflakeServer.Serve()
		t.Cleanup(func() { snowflakeServer.Close() })
		database, err := types.NewDatabaseV3(types.Metadata{
			Name: name,
		}, types.DatabaseSpecV3{
			Protocol:      defaults.ProtocolSnowflake,
			URI:           net.JoinHostPort("localhost.snowflakecomputing.com", snowflakeServer.Port()),
			DynamicLabels: dynamicLabels,
		})
		require.NoError(t, err)
		testCtx.snowflake[name] = testSnowflake{
			db:       snowflakeServer,
			resource: database,
		}
		return database
	}
}
