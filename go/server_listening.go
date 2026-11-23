package nocontract

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"time"

	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/sync/errgroup"
)

// ListenOptions names the TCP address and request logger a generated server uses. Its fields
// belong to the runtime, so another server option preserves the generated Listen signature.
type ListenOptions struct {
	address       string
	requestLogger outcome.Option[*slog.Logger]
}

// ListenOn chooses a TCP address, such as "127.0.0.1:8080". Port zero asks the operating system
// for a free port; ListeningServer.Address reports the address it assigned.
func ListenOn(address string) ListenOptions {
	return ListenOptions{address: address, requestLogger: outcome.None[*slog.Logger]()}
}

// RouteBinding is the declared endpoints and layers a generated API owns. It accepts typed
// answers, and cannot hold an HTTP handler or response writer.
type RouteBinding struct {
	endpoints []Endpoint
	layers    []Layer
}

// BindRoutes binds a generated route table to its layers, copying both collections so their
// caller cannot replace a route or layer after the API is built.
func BindRoutes(endpoints []Endpoint, layers []Layer) RouteBinding {
	return RouteBinding{endpoints: slices.Clone(endpoints), layers: slices.Clone(layers)}
}

// ListeningServer owns one generated API's TCP listener and the task serving it. Only Listen
// builds one; applications can read its bound address, wait for it, and stop it.
type ListeningServer struct {
	server *http.Server
	tasks  *errgroup.Group
}

// ServerStopped is a listener that has stopped accepting requests at Address.
//
//ergo:not-an-error
type ServerStopped struct {
	Address string
}

// ListenerNotOpened is a TCP address the operating system could not listen on, with its cause.
//
//ergo:code NC-1140
//ergo:message The server could not listen on {Address}.
type ListenerNotOpened struct {
	Address string
	Cause   error
}

// Error describes the address that could not be opened.
func (failure ListenerNotOpened) Error() string {
	return fmt.Sprintf("listening on %s: %v", failure.Address, failure.Cause)
}

// Unwrap preserves the operating system's failure.
func (failure ListenerNotOpened) Unwrap() error { return failure.Cause }

// ListenerNotServed is a bound listener whose HTTP serving task stopped with a cause.
//
//ergo:code NC-1141
//ergo:message The server on {Address} could not keep serving requests.
type ListenerNotServed struct {
	Address string
	Cause   error
}

// Error describes the bound listener that stopped serving.
func (failure ListenerNotServed) Error() string {
	return fmt.Sprintf("serving on %s: %v", failure.Address, failure.Cause)
}

// Unwrap preserves the HTTP serving failure.
func (failure ListenerNotServed) Unwrap() error { return failure.Cause }

// ListenerNotStopped is a server whose requests did not finish during the shutdown context.
//
//ergo:code NC-1142
//ergo:message The server on {Address} could not finish stopping.
type ListenerNotStopped struct {
	Address string
	Cause   error
}

// Error describes the server that could not finish stopping.
func (failure ListenerNotStopped) Error() string {
	return fmt.Sprintf("stopping the server on %s: %v", failure.Address, failure.Cause)
}

// Unwrap preserves the shutdown failure.
func (failure ListenerNotStopped) Unwrap() error { return failure.Cause }

// Listen starts the generated binding on its chosen TCP address. The context governs opening
// the listener and the requests it serves. Stop owns shutdown; Wait reports the serving task's
// terminal result. Neither operation exposes the underlying HTTP server or its handler.
func Listen(serverContext context.Context, options ListenOptions, binding RouteBinding) outcome.Result[ListeningServer] {
	configuration := net.ListenConfig{
		Control: nil, KeepAlive: 0,
		KeepAliveConfig: net.KeepAliveConfig{Enable: false, Idle: 0, Interval: 0, Count: 0},
	}
	return outcome.Fold(outcome.TupleToResult(configuration.Listen(serverContext, "tcp", options.address)), func(listener net.Listener) outcome.Result[ListeningServer] {
		server := newHTTPServer(serverContext, options, binding, listener.Addr().String())
		tasks := &errgroup.Group{}
		tasks.Go(func() error { return serveListener(server, listener) })
		return outcome.Ok(ListeningServer{server: server, tasks: tasks})
	}, func(cause error) outcome.Result[ListeningServer] {
		return outcome.Err[ListeningServer](ListenerNotOpened{Address: options.address, Cause: cause})
	})
}

func newHTTPServer(serverContext context.Context, options ListenOptions, binding RouteBinding, address string) *http.Server {
	return &http.Server{
		Addr: address, Handler: boundRoutes{binding: binding, requestLogger: options.requestLogger, clock: time.Now}, DisableGeneralOptionsHandler: true,
		TLSConfig: nil, ReadTimeout: 0, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 0, IdleTimeout: 0,
		MaxHeaderBytes: 0, MaxHeaderValueCount: 0, TLSNextProto: nil, ConnState: nil, ErrorLog: nil,
		BaseContext: func(net.Listener) context.Context { return serverContext }, ConnContext: nil,
		HTTP2: nil, Protocols: nil, DisableClientPriority: false,
	}
}

func serveListener(server *http.Server, listener net.Listener) error {
	cause := server.Serve(listener)
	if errors.Is(cause, http.ErrServerClosed) {
		return nil
	}
	return ListenerNotServed{Address: server.Addr, Cause: cause}
}

// Address is the TCP address the operating system assigned to this listener.
func (listening ListeningServer) Address() string { return listening.server.Addr }

// Wait waits for this listener's serving task and preserves any failure that stopped it.
func (listening ListeningServer) Wait() outcome.Result[ServerStopped] {
	return outcome.FoldOption(outcome.EmptyableToOption(listening.tasks.Wait()), outcome.Err[ServerStopped], func() outcome.Result[ServerStopped] {
		return outcome.Ok(ServerStopped{Address: listening.server.Addr})
	})
}

// Stop closes the listener and waits for active requests through net/http's shutdown owner.
// Concurrent Stop and Wait calls share that same server and serving task.
func (listening ListeningServer) Stop(context context.Context) outcome.Result[ServerStopped] {
	return outcome.FoldOption(outcome.EmptyableToOption(listening.server.Shutdown(context)), func(cause error) outcome.Result[ServerStopped] {
		return outcome.Err[ServerStopped](ListenerNotStopped{Address: listening.server.Addr, Cause: cause})
	}, func() outcome.Result[ServerStopped] {
		return listening.Wait()
	})
}

type boundRoutes struct {
	binding       RouteBinding
	requestLogger outcome.Option[*slog.Logger]
	clock         func() time.Time
}

func (bound boundRoutes) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	respond(writer, request, bound.binding.endpoints, bound.binding.layers, bound.requestLogger, bound.clock)
}
