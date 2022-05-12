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

package snowflake

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"time"

	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/lib/auth"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/services"
	"github.com/gravitational/teleport/lib/srv/db/common"
	"github.com/gravitational/teleport/lib/srv/db/common/role"
	"github.com/gravitational/teleport/lib/utils"
	"github.com/gravitational/trace"
)

func init() {
	common.RegisterEngine(newEngine, defaults.ProtocolSnowflake)
}

// newEngine create new Redis engine.
func newEngine(ec common.EngineConfig) common.Engine {
	return &Engine{
		EngineConfig: ec,
		HttpClient:   getDefaultHttpClient(),
	}
}

type Engine struct {
	// EngineConfig is the common database engine configuration.
	common.EngineConfig
	// clientConn is a client connection.
	clientConn net.Conn
	// sessionCtx is current session context.
	sessionCtx *common.Session
	// HttpClient is the client being used to talk to Snowflake API.
	HttpClient *http.Client

	connectionToken string
}

func (e *Engine) InitializeConnection(clientConn net.Conn, sessionCtx *common.Session) error {
	e.clientConn = clientConn
	e.sessionCtx = sessionCtx

	return nil
}

func getDefaultHttpClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}
}

func (e *Engine) SendError(err error) {
	if err == nil || utils.IsOKNetworkError(err) {
		return
	}

	e.Log.Errorf("snowflake error: %+v", trace.Unwrap(err)) // TODO(jakule): remove log

	if e.clientConn == nil {
		return
	}

	// Assume internal server error HTTP 500 and override if possible.
	statusCode := http.StatusInternalServerError
	if trace.IsAccessDenied(err) {
		statusCode = http.StatusUnauthorized
	}

	jsonBody := fmt.Sprintf(`{"success": false, "message:"%s"}`, err.Error())

	response := &http.Response{
		ProtoMajor: 1,
		ProtoMinor: 1,
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(jsonBody)),
		Header: map[string][]string{
			"Content-Type": {"application/json"},
		},
	}

	dumpResponse, err := httputil.DumpResponse(response, true)
	if err != nil {
		e.Log.Errorf("snowflake error: %+v", trace.Unwrap(err))
		return
	}

	_, err = e.clientConn.Write(dumpResponse)
	if err != nil {
		e.Log.Errorf("snowflake error: %+v", trace.Unwrap(err))
		return
	}
}

func (e *Engine) HandleConnection(ctx context.Context, sessionCtx *common.Session) error {
	accountName, err := extractAccountName(sessionCtx.Database.GetURI())
	if err != nil {
		return trace.Wrap(err)
	}

	if err := e.authorizeConnection(ctx); err != nil {
		return trace.Wrap(err)
	}

	e.Audit.OnSessionStart(e.Context, sessionCtx, nil)
	defer e.Audit.OnSessionEnd(e.Context, sessionCtx)

	clientConnReader := bufio.NewReader(e.clientConn)

	for {
		req, err := http.ReadRequest(clientConnReader)
		if err != nil {
			return trace.Wrap(err)
		}

		err = e.processRequest(ctx, sessionCtx, req, accountName)
		if err != nil {
			return trace.Wrap(err)
		}
	}
}

// extractAccountName extracts account name from provided Snowflake URL
// ref: https://docs.snowflake.com/en/user-guide/admin-account-identifier.html
func extractAccountName(uri string) (string, error) {
	if !strings.Contains(uri, "snowflakecomputing.com") {
		return "", trace.Errorf("Snowflake address should contain snowflakecomputing.com")
	}

	if strings.HasPrefix(uri, "https://") {
		uri = strings.TrimSuffix(uri, "https://")
	}

	uriParts := strings.Split(uri, ".")

	switch len(uriParts) {
	case 3:
		// address in https://test.snowflakecomputing.com format
		return uriParts[0], nil
	case 5:
		// address in https://test.us-east-2.aws.snowflakecomputing.com format
		return strings.Join(uriParts[:3], "."), nil
	default:
		return "", trace.BadParameter("invalid Snowflake url: %s", uri)
	}
}

func (e *Engine) processRequest(ctx context.Context, sessionCtx *common.Session, req *http.Request, accountName string) error {
	var err error
	if e.connectionToken == "" {
		e.connectionToken, err = e.getConnectionToken(ctx, req)
		if err != nil {
			return trace.Wrap(err)
		}
	}

	requestBodyReader, err := e.process(ctx, req, accountName)
	if err != nil {
		return trace.Wrap(err)
	}

	reqCopy, err := e.copyRequest(ctx, req, requestBodyReader)
	if err != nil {
		return trace.Wrap(err)
	}

	reqCopy.URL.Scheme = "https"
	reqCopy.URL.Host = sessionCtx.Database.GetURI()

	e.setAuthorizationHeader(reqCopy)

	// Send the request to Snowflake API
	resp, err := e.HttpClient.Do(reqCopy)
	if err != nil {
		return trace.Wrap(err)
	}
	defer resp.Body.Close()

	switch req.URL.Path {
	case loginRequestPath:
		err := e.processResponse(resp, func(body []byte) ([]byte, error) {
			newPayload, err := e.saveSessionToken(ctx, e.sessionCtx, body, accountName)
			return newPayload, trace.Wrap(err)
		})

		// Return here - processLoginResponse sends the response.
		return trace.Wrap(err)
	case tokenRequestPath:
		err := e.processResponse(resp, func(body []byte) ([]byte, error) {
			renewSessResp := &renewSessionResponse{}
			if err := json.Unmarshal(body, renewSessResp); err != nil {
				return nil, trace.Wrap(err)
			}

			if renewSessResp.Data.SessionToken != "" {
				snowflakeSession, err := e.AuthClient.CreateSnowflakeSession(ctx, types.CreateSnowflakeSessionRequest{
					Username:             sessionCtx.Identity.Username,
					SnowflakeAccountName: accountName,
					SnowflakeUsername:    sessionCtx.DatabaseUser,
					SessionToken:         renewSessResp.Data.SessionToken,
				})
				if err != nil {
					return nil, trace.Wrap(err)
				}

				e.connectionToken = renewSessResp.Data.SessionToken
				renewSessResp.Data.SessionToken = snowflakeSession.GetName()
			}

			newBody, err := json.Marshal(renewSessResp)
			return newBody, trace.Wrap(err)
		})

		// Return here - processLoginResponse sends the response.
		return trace.Wrap(err)
	}

	return trace.Wrap(e.sendResponse(resp))
}

func (e *Engine) sendResponse(resp *http.Response) error {
	dumpResp, err := httputil.DumpResponse(resp, false)
	if err != nil {
		return trace.Wrap(err)
	}

	_, err = e.clientConn.Write(dumpResp)
	if err != nil {
		return trace.Wrap(err)
	}

	if _, err := io.Copy(e.clientConn, resp.Body); err != nil {
		return trace.Wrap(err)
	}
	return nil
}

func (e *Engine) setAuthorizationHeader(reqCopy *http.Request) {
	if e.connectionToken != "" {
		reqCopy.Header.Set("Authorization", fmt.Sprintf("Snowflake Token=\"%s\"", e.connectionToken))
	} else {
		if reqCopy.Header.Get("Authorization") == "Basic" {
			reqCopy.Header.Del("Authorization")
		}
	}
}

func (e *Engine) processResponse(resp *http.Response, modifyReqFn func(body []byte) ([]byte, error)) error {
	dumpResp, err := httputil.DumpResponse(resp, false)
	if err != nil {
		return trace.Wrap(err)
	}

	if resp.StatusCode != http.StatusOK {
		e.Log.Warnf("Not 200 response code: %d", resp.StatusCode)
	} else {
		var bodyReader io.Reader
		if resp.Header.Get("Content-Encoding") == "gzip" {
			gzipReader, err := gzip.NewReader(resp.Body)
			if err != nil {
				return err
			}
			defer gzipReader.Close()

			bodyReader = gzipReader
		} else {
			bodyReader = resp.Body
		}

		// TODO(jakule) add limiter
		body, err := io.ReadAll(bodyReader)
		if err != nil {
			return trace.Wrap(err)
		}

		newPayload, err := modifyReqFn(body)
		if err != nil {
			return trace.Wrap(err)
		}

		buf, err := writeResponse(resp, newPayload)
		if err != nil {
			return trace.Wrap(err)
		}

		dumpResp, err = copyResponse(resp, buf.Bytes())
		if err != nil {
			return trace.Wrap(err)
		}
	}

	_, err = e.clientConn.Write(dumpResp)
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

func writeResponse(resp *http.Response, newResp []byte) (*bytes.Buffer, error) {
	buf := &bytes.Buffer{}
	if resp.Header.Get("Content-Encoding") == "gzip" {
		newGzBody := gzip.NewWriter(buf)

		if _, err := newGzBody.Write(newResp); err != nil {
			return nil, trace.Wrap(err)
		}

		if err := newGzBody.Close(); err != nil {
			return nil, trace.Wrap(err)
		}
	} else {
		buf.Write(newResp)
	}
	return buf, nil
}

// authorizeConnection does authorization check for Snowflake connection about
// to be established.
func (e *Engine) authorizeConnection(ctx context.Context) error {
	ap, err := e.Auth.GetAuthPreference(ctx)
	if err != nil {
		return trace.Wrap(err)
	}
	mfaParams := services.AccessMFAParams{
		Verified:       e.sessionCtx.Identity.MFAVerified != "",
		AlwaysRequired: ap.GetRequireSessionMFA(),
	}

	dbRoleMatchers := role.DatabaseRoleMatchers(
		e.sessionCtx.Database.GetProtocol(),
		e.sessionCtx.DatabaseUser,
		e.sessionCtx.DatabaseName,
	)
	err = e.sessionCtx.Checker.CheckAccess(
		e.sessionCtx.Database,
		mfaParams,
		dbRoleMatchers...,
	)
	if err != nil {
		e.Audit.OnSessionStart(e.Context, e.sessionCtx, err)
		return trace.Wrap(err)
	}
	return nil
}

func (e *Engine) saveSessionToken(ctx context.Context, sessionCtx *common.Session, respBody []byte, accountName string) ([]byte, error) {
	if newResp, err := e.extractToken(respBody, func(sessionToken string) (string, error) {
		snowflakeSession, err := e.AuthClient.CreateSnowflakeSession(ctx, types.CreateSnowflakeSessionRequest{
			Username:             sessionCtx.Identity.Username,
			SnowflakeAccountName: accountName,
			SnowflakeUsername:    sessionCtx.DatabaseUser,
			SessionToken:         e.connectionToken,
		})
		if err != nil {
			return "", trace.Wrap(err)
		}

		return snowflakeSession.GetName(), nil
	}); err == nil {
		e.Log.Debugf("extracted token")

		return newResp, nil
	} else {
		e.Log.Debugf("failed to extract token: %v", err)
		return nil, trace.Wrap(err)
	}
}

func (e *Engine) copyRequest(ctx context.Context, req *http.Request, body io.Reader) (*http.Request, error) {
	reqCopy, err := http.NewRequestWithContext(ctx, req.Method, req.URL.String(), body)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	for k, v := range req.Header {
		reqCopy.Header.Set(k, strings.Join(v, ","))
	}

	return reqCopy, nil
}

func (e *Engine) process(ctx context.Context, req *http.Request, accountName string) (io.Reader, error) {
	var newBody io.Reader

	switch req.URL.Path {
	case loginRequestPath:
		jwtToken, err := e.AuthClient.GenerateDatabaseJWT(ctx, types.GenerateSnowflakeJWT{
			Username: e.sessionCtx.DatabaseUser,
			Account:  accountName,
		})
		if err != nil {
			return nil, trace.Wrap(err)
		}

		newBody, err = e.modifyRequestBody(req, func(body []byte) ([]byte, error) {
			newBody, err := replaceToken(body, jwtToken, accountName)
			if err != nil {
				return nil, trace.Wrap(err)
			}

			return newBody, nil
		})

		if err != nil {
			return nil, trace.Wrap(err)
		}
	case queryRequestPath:
		var err error
		newBody, err = e.modifyRequestBody(req, func(body []byte) ([]byte, error) {
			query, err := extractSQLStmt(body)
			if err != nil {
				return nil, trace.Wrap(err, "failed to extract SQL query")
			}
			// TODO(jakule): Add request ID??
			e.Audit.OnQuery(ctx, e.sessionCtx, common.Query{Query: query})

			return body, nil
		})

		if err != nil {
			return nil, trace.Wrap(err)
		}
	case tokenRequestPath:
		var err error
		newBody, err = e.modifyRequestBody(req, func(body []byte) ([]byte, error) {
			refreshReq := &renewSessionRequest{}
			if err := json.Unmarshal(body, &refreshReq); err != nil {
				return nil, trace.Wrap(err)
			}

			refreshReq.OldSessionToken = e.connectionToken

			newData, err := json.Marshal(refreshReq)

			return newData, trace.Wrap(err)
		})

		if err != nil {
			return nil, trace.Wrap(err)
		}
	default:
		newBody = req.Body
	}

	return newBody, nil
}

func (e *Engine) modifyRequestBody(req *http.Request, modifyReqFn func(body []byte) ([]byte, error)) (*bytes.Buffer, error) {
	body, err := readRequestBody(req)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if newBody, err := modifyReqFn(body); err == nil {
		e.Log.Debugf("new body: %s", string(newBody))

		body = newBody
	} else {
		e.Log.Errorf("failed to unmarshal login JSON: %v", err) // TODO(jakule)

		return nil, trace.Wrap(err)
	}

	buf := &bytes.Buffer{}
	if req.Header.Get("Content-Encoding") == "gzip" {
		newGzBody := gzip.NewWriter(buf)

		if _, err := newGzBody.Write(body); err != nil {
			return nil, trace.Wrap(err)
		}

		if err := newGzBody.Close(); err != nil {
			return nil, trace.Wrap(err)
		}
	} else {
		buf.Write(body)
	}

	req.Header.Set("Content-Length", strconv.Itoa(buf.Len()))

	return buf, nil
}

func readRequestBody(req *http.Request) ([]byte, error) {
	// TODO(jakule): Add limiter
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if req.Header.Get("Content-Encoding") == "gzip" {
		bodyGZ, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, trace.Wrap(err)
		}

		body, err = io.ReadAll(bodyGZ)
		if err != nil {
			return nil, trace.Wrap(err)
		}
	}

	return body, nil
}

func (e *Engine) getConnectionToken(ctx context.Context, req *http.Request) (string, error) {
	var connectionToken string

	if strings.Contains(req.Header.Get("Authorization"), "Snowflake Token") &&
		!strings.Contains(req.Header.Get("Authorization"), "None") {

		sessionID := extractSnowflakeToken(req)

		if err := auth.WaitForSnowflakeSession(ctx, sessionID, e.sessionCtx.Identity.Username, e.AuthClient); err != nil {
			return "", trace.Wrap(err)
		}

		snowflakeSession, err := e.AuthClient.GetSnowflakeSession(ctx, types.GetSnowflakeSessionRequest{
			SessionID: sessionID,
		})
		if err != nil {
			return "", trace.Wrap(err)
		}

		connectionToken = snowflakeSession.GetBearerToken()
	}

	return connectionToken, nil
}

func extractSnowflakeToken(req *http.Request) string {
	sessionID := req.Header.Get("Authorization")
	return extractSnowflakeTokenFromHeader(sessionID)
}

func extractSnowflakeTokenFromHeader(token string) string {
	sessionID := strings.TrimPrefix(token, "Snowflake Token=\"")
	sessionID = strings.TrimSuffix(sessionID, "\"")
	return sessionID
}

func copyResponse(resp *http.Response, body []byte) ([]byte, error) {
	resp.Body = io.NopCloser(bytes.NewBuffer(body))
	resp.ContentLength = int64(len(body))
	if _, ok := resp.Header["Content-Length"]; ok {
		delete(resp.Header, "Content-Length")
	}
	return httputil.DumpResponse(resp, true)
}

type queryRequest struct {
	SQLText string `json:"sqlText"`
}

func extractSQLStmt(body []byte) (string, error) {
	queryRequest := &queryRequest{}
	if err := json.Unmarshal(body, queryRequest); err != nil {
		return "", trace.Wrap(err)
	}

	return queryRequest.SQLText, nil
}

func (e *Engine) extractToken(bodyBytes []byte, sessCb func(string) (string, error)) ([]byte, error) {
	loginResp := &LoginResponse{}
	if err := json.Unmarshal(bodyBytes, loginResp); err != nil {
		return nil, trace.Wrap(err)
	}

	//e.Log.Warnf("login response: %s", bodyBytes)

	if loginResp.Success == false {
		return nil, trace.Errorf("snowflake authentication failed: %s", loginResp.Message)
	}

	dataToken, found := loginResp.Data["token"]
	if !found {
		return nil, trace.Errorf("")
	}

	connectionToken, ok := dataToken.(string)
	if !ok {
		return nil, trace.Errorf("session token returned by Snowflake API expected to be a string, got %T", dataToken)
	}

	e.connectionToken = extractSnowflakeTokenFromHeader(connectionToken)

	sessionToken, err := sessCb(e.connectionToken)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	loginResp.Data["token"] = sessionToken
	loginResp.Data["masterToken"] = sessionToken //TODO(jakule)

	newResp, err := json.Marshal(loginResp)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	//e.Log.Warnf("login new response: %s", newResp)

	return newResp, err
}

func replaceToken(loginReq []byte, jwtToken string, accountName string) ([]byte, error) {
	logReq := &LoginRequest{}
	if err := json.Unmarshal(loginReq, logReq); err != nil {
		return nil, trace.Wrap(err)
	}

	logReq.Data["TOKEN"] = jwtToken
	logReq.Data["ACCOUNT_NAME"] = accountName
	logReq.Data["AUTHENTICATOR"] = "SNOWFLAKE_JWT"

	if _, ok := logReq.Data["PASSWORD"]; ok {
		delete(logReq.Data, "PASSWORD")
	}

	if _, ok := logReq.Data["EXT_AUTHN_DUO_METHOD"]; ok {
		delete(logReq.Data, "EXT_AUTHN_DUO_METHOD")
	}

	return json.Marshal(logReq)
}

type LoginResponse struct {
	Data map[string]interface{} `json:"data"`
	//Data struct {
	//	MasterToken             string      `json:"masterToken"`
	//	Token                   string      `json:"token"`
	//	ValidityInSeconds       int         `json:"validityInSeconds"`
	//	MasterValidityInSeconds int         `json:"masterValidityInSeconds"`
	//	DisplayUserName         string      `json:"displayUserName"`
	//	ServerVersion           string      `json:"serverVersion"`
	//	FirstLogin              bool        `json:"firstLogin"`
	//	RemMeToken              interface{} `json:"remMeToken"`
	//	RemMeValidityInSeconds  int         `json:"remMeValidityInSeconds"`
	//	HealthCheckInterval     int         `json:"healthCheckInterval"`
	//	NewClientForUpgrade     interface{} `json:"newClientForUpgrade"`
	//	SessionID               int64       `json:"sessionId"`
	//	//Parameters              []struct {
	//	//	Name  string `json:"name"`
	//	//	Value int    `json:"value"`
	//	//} `json:"parameters"`
	//	SessionInfo struct {
	//		DatabaseName  interface{} `json:"databaseName"`
	//		SchemaName    interface{} `json:"schemaName"`
	//		WarehouseName interface{} `json:"warehouseName"`
	//		RoleName      string      `json:"roleName"`
	//	} `json:"sessionInfo"`
	//	IDToken                   interface{} `json:"idToken"`
	//	IDTokenValidityInSeconds  int         `json:"idTokenValidityInSeconds"`
	//	ResponseData              interface{} `json:"responseData"`
	//	MfaToken                  interface{} `json:"mfaToken"`
	//	MfaTokenValidityInSeconds int         `json:"mfaTokenValidityInSeconds"`
	//} `json:"data"`
	Code    interface{} `json:"code"`
	Message interface{} `json:"message"`
	Success bool        `json:"success"`
}

type LoginRequest struct {
	//Data struct {
	//	ClientAppID       string      `json:"CLIENT_APP_ID"`
	//	ClientAppVersion  string      `json:"CLIENT_APP_VERSION"`
	//	SvnRevision       interface{} `json:"SVN_REVISION"`
	//	AccountName       string      `json:"ACCOUNT_NAME"`
	//	LoginName         string      `json:"LOGIN_NAME"`
	//	ClientEnvironment struct {
	//		Application    string      `json:"APPLICATION"`
	//		Os             string      `json:"OS"`
	//		OsVersion      string      `json:"OS_VERSION"`
	//		PythonVersion  string      `json:"PYTHON_VERSION"`
	//		PythonRuntime  string      `json:"PYTHON_RUNTIME"`
	//		PythonCompiler string      `json:"PYTHON_COMPILER"`
	//		OcspMode       string      `json:"OCSP_MODE"`
	//		Tracing        interface{} `json:"TRACING"`
	//		LoginTimeout   int         `json:"LOGIN_TIMEOUT"`
	//		NetworkTimeout interface{} `json:"NETWORK_TIMEOUT"`
	//	} `json:"CLIENT_ENVIRONMENT"`
	//	Authenticator     string `json:"AUTHENTICATOR"`
	//	Token             string `json:"TOKEN"`
	//	SessionParameters struct {
	//		AbortDetachedQuery     bool `json:"ABORT_DETACHED_QUERY"`
	//		Autocommit             bool `json:"AUTOCOMMIT"`
	//		ClientSessionKeepAlive bool `json:"CLIENT_SESSION_KEEP_ALIVE"`
	//		ClientPrefetchThreads  int  `json:"CLIENT_PREFETCH_THREADS"`
	//	} `json:"SESSION_PARAMETERS"`
	//} `json:"data"`
	Data map[string]interface{} `json:"data"`
}

type RefreshTokenRequest struct {
	OldSessionToken string `json:"oldSessionToken"`
	RequestType     string `json:"requestType"`
}

type renewSessionRequest struct {
	OldSessionToken string `json:"oldSessionToken"`
	RequestType     string `json:"requestType"` // "RENEW"
}

type renewSessionResponse struct {
	Data    renewSessionResponseMain `json:"data"`
	Message string                   `json:"message"`
	Code    string                   `json:"code"`
	Success bool                     `json:"success"`
}

type renewSessionResponseMain struct {
	SessionToken        string        `json:"sessionToken"`
	ValidityInSecondsST time.Duration `json:"validityInSecondsST"`
	MasterToken         string        `json:"masterToken"`
	ValidityInSecondsMT time.Duration `json:"validityInSecondsMT"`
	SessionID           int64         `json:"sessionId"`
}
