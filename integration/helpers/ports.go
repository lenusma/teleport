/*
Copyright 2021 Gravitational, Inc.

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

package helpers

import (
	"net"
	"strconv"
	"testing"

	"github.com/gravitational/teleport/lib/service"
	"github.com/stretchr/testify/require"
)

// ports contains tcp ports allocated for all integration tests.
// TODO: Replace all usage of `Ports` with FD-injected sockets as per
//       https://github.com/gravitational/teleport/pull/13346
//var ports utils.PortList

// func init() {
// 	// Allocate tcp ports for all integration tests. 5000 should be plenty.
// 	var err error
// 	ports, err = utils.GetFreeTCPPorts(5000, utils.PortStartingNumber)
// 	if err != nil {
// 		panic(fmt.Sprintf("failed to allocate tcp ports for tests: %v", err))
// 	}
// }

// func NewPortValue() int {
// 	return ports.PopInt()
// }

// func NewPortStr() string {
// 	return ports.Pop()
// }

// func NewPortSlice(n int) []int {
// 	return ports.PopIntSlice(n)
// }

// func NewInstancePort() *InstancePort {
// 	i := ports.PopInt()
// 	p := InstancePort(i)
// 	return &p
// }

// type InstancePort int

// func (p *InstancePort) String() string {
// 	if p == nil {
// 		return ""
// 	}
// 	return strconv.Itoa(int(*p))
// }

// func SingleProxyPortSetup() *InstancePorts {
// 	v := NewInstancePort()
// 	return &InstancePorts{
// 		Web:               v,
// 		SSHProxy:          v,
// 		ReverseTunnel:     v,
// 		MySQL:             v,
// 		SSH:               NewInstancePort(),
// 		Auth:              NewInstancePort(),
// 		isSinglePortSetup: true,
// 	}
// }

type InstanceListeners struct {
	Web               string
	SSH               string
	SSHProxy          string
	Auth              string
	ReverseTunnel     string
	MySQL             string
	Postgres          string
	Mongo             string
	IsSinglePortSetup bool
}

func StandardListenerSetup(t *testing.T, fds *[]service.FileDescriptor) *InstanceListeners {
	return &InstanceListeners{
		Web:           NewListener(t, service.ListenerProxyWeb, fds),
		SSH:           NewListener(t, service.ListenerNodeSSH, fds),
		Auth:          NewListener(t, service.ListenerAuthSSH, fds),
		SSHProxy:      NewListener(t, service.ListenerProxySSH, fds),
		ReverseTunnel: NewListener(t, service.ListenerProxyTunnel, fds),
		MySQL:         NewListener(t, service.ListenerProxyMySQL, fds),
	}
}

// func StandardPortSetup() *InstancePorts {
// 	return &InstancePorts{
// 		Web:           NewInstancePort(),
// 		SSH:           NewInstancePort(),
// 		Auth:          NewInstancePort(),
// 		SSHProxy:      NewInstancePort(),
// 		ReverseTunnel: NewInstancePort(),
// 		MySQL:         NewInstancePort(),
// 	}
// }

// func WebReverseTunnelMuxPortSetup() *InstancePorts {
// 	v := NewInstancePort()
// 	return &InstancePorts{
// 		Web:           v,
// 		ReverseTunnel: v,
// 		SSH:           NewInstancePort(),
// 		SSHProxy:      NewInstancePort(),
// 		MySQL:         NewInstancePort(),
// 		Auth:          NewInstancePort(),
// 	}
// }

// func SeparatePostgresPortSetup() *InstancePorts {
// 	return &InstancePorts{
// 		Web:           NewInstancePort(),
// 		SSH:           NewInstancePort(),
// 		Auth:          NewInstancePort(),
// 		SSHProxy:      NewInstancePort(),
// 		ReverseTunnel: NewInstancePort(),
// 		MySQL:         NewInstancePort(),
// 		Postgres:      NewInstancePort(),
// 	}
// }

// func SeparateMongoPortSetup() *InstancePorts {
// 	return &InstancePorts{
// 		Web:           NewInstancePort(),
// 		SSH:           NewInstancePort(),
// 		Auth:          NewInstancePort(),
// 		SSHProxy:      NewInstancePort(),
// 		ReverseTunnel: NewInstancePort(),
// 		MySQL:         NewInstancePort(),
// 		Mongo:         NewInstancePort(),
// 	}
// }

// type InstancePorts struct {
// 	Host string
// 	Web  *InstancePort
// 	// SSH is an instance of SSH server Port.
// 	SSH *InstancePort
// 	// SSHProxy is Teleport SSH Proxy Port.
// 	SSHProxy      *InstancePort
// 	Auth          *InstancePort
// 	ReverseTunnel *InstancePort
// 	MySQL         *InstancePort
// 	Postgres      *InstancePort
// 	Mongo         *InstancePort

// 	isSinglePortSetup bool
// }

func Port(t *testing.T, addr string) int {
	t.Helper()

	_, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	return port
}

// func (i *InstancePorts) GetPortSSHInt() int           { return int(*i.SSH) }
// func (i *InstancePorts) GetPortSSH() string           { return i.SSH.String() }
// func (i *InstancePorts) GetPortAuth() string          { return i.Auth.String() }
// func (i *InstancePorts) GetPortProxy() string         { return i.SSHProxy.String() }
// func (i *InstancePorts) GetPortWeb() string           { return i.Web.String() }
// func (i *InstancePorts) GetPortMySQL() string         { return i.MySQL.String() }
// func (i *InstancePorts) GetPortPostgres() string      { return i.Postgres.String() }
// func (i *InstancePorts) GetPortMongo() string         { return i.Mongo.String() }
// func (i *InstancePorts) GetPortReverseTunnel() string { return i.ReverseTunnel.String() }

// func (i *InstancePorts) GetSSHAddr() string {
// 	if i.SSH == nil {
// 		return ""
// 	}
// 	return net.JoinHostPort(i.Host, i.GetPortSSH())
// }

// func (i *InstancePorts) GetAuthAddr() string {
// 	if i.Auth == nil {
// 		return ""
// 	}
// 	return net.JoinHostPort(i.Host, i.GetPortAuth())
// }

// func (i *InstancePorts) GetProxyAddr() string {
// 	if i.SSHProxy == nil {
// 		return ""
// 	}
// 	return net.JoinHostPort(i.Host, i.GetPortProxy())
// }

// func (i *InstancePorts) GetWebAddr() string {
// 	if i.Web == nil {
// 		return ""
// 	}
// 	return net.JoinHostPort(i.Host, i.GetPortWeb())
// }

// func (i *InstancePorts) GetMySQLAddr() string {
// 	if i.MySQL == nil {
// 		return ""
// 	}
// 	return net.JoinHostPort(i.Host, i.GetPortMySQL())
// }

// func (i *InstancePorts) GetReverseTunnelAddr() string {
// 	if i.ReverseTunnel == nil {
// 		return ""
// 	}
// 	return net.JoinHostPort(i.Host, i.GetPortReverseTunnel())
// }

// NewListener creates a new TCP listener on 127.0.0.1:0, adds it to the
// FileDescriptor slice (with the specified type) and returns its actual local
// address as a string (for use in configuration). The idea is to subvert
// Teleport's file-descriptor injection mechanism (used to share ports between
// parent and child processes) to inject preconfigured listeners to Teleport
// instances under test. The ports are allocated and bound at runtime, so there
// should be no issues with port clashes on parallel tests.
//
// The resulting file descriptor is added to the `fds` slice, which can then be
// given to a teleport instance on startup in order to suppl
func NewListener(t *testing.T, ty service.ListenerType, fds *[]service.FileDescriptor) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	addr := l.Addr().String()

	// File() returns a dup of the listener's file descriptor as an *os.File, so
	// the original net.Listener still needs to be closed.
	lf, err := l.(*net.TCPListener).File()
	require.NoError(t, err)

	t.Logf("Listener %s for %s", addr, ty)

	// If the file descriptor slice ends up being passed to a TeleportProcess
	// that successfully starts, listeners will either get "imported" and used
	// or discarded and closed, this is just an extra safety measure that closes
	// the listener at the end of the test anyway (the finalizer would do that
	// anyway, in principle).
	t.Cleanup(func() { lf.Close() })

	*fds = append(*fds, service.FileDescriptor{
		Type:    string(ty),
		Address: addr,
		File:    lf,
	})

	return addr
}
