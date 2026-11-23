package nocontract

import (
	"errors"
	"net"
	"testing"
	"testing/synctest"

	"github.com/CheckPickerUpper/outcome"
	"golang.org/x/sync/errgroup"
)

func TestStopWaitsForTheBoundListenerBeforeReportingSuccess(t *testing.T) {
	configuration := net.ListenConfig{
		Control: nil, KeepAlive: 0,
		KeepAliveConfig: net.KeepAliveConfig{Enable: false, Idle: 0, Interval: 0, Count: 0},
	}
	outcome.Fold(outcome.TupleToResult(configuration.Listen(t.Context(), "tcp", "127.0.0.1:0")), func(listener net.Listener) struct{} {
		defer func() {
			outcome.ForEachOption(outcome.EmptyableToOption(listener.Close()), func(cause error) {
				if !errors.Is(cause, net.ErrClosed) {
					t.Errorf("closing the test listener: %v", cause)
				}
			})
		}()
		synctest.Test(t, func(t *testing.T) {
			beginServing, shutdownStarted := make(chan struct{}), make(chan struct{})
			server := newHTTPServer(t.Context(), ListenOn(listener.Addr().String()), BindRoutes([]Endpoint{}, []Layer{}), listener.Addr().String())
			server.RegisterOnShutdown(func() { close(shutdownStarted) })
			serving := &errgroup.Group{}
			serving.Go(func() error {
				<-beginServing
				return serveListener(server, listener)
			})
			listening := ListeningServer{server: server, tasks: serving}
			stopped := make(chan outcome.Result[ServerStopped], 1)
			stopping := &errgroup.Group{}
			stopping.Go(func() error {
				stopped <- listening.Stop(t.Context())
				return nil
			})
			<-shutdownStarted
			synctest.Wait()
			select {
			case <-stopped:
				close(beginServing)
				t.Fatal("Stop reported success while its bound listener was still waiting to serve")
			default:
				close(beginServing)
			}
			outcome.Fold(<-stopped, func(closed ServerStopped) struct{} {
				if closed.Address != listener.Addr().String() {
					t.Errorf("stopped listener = %+v; want %s", closed, listener.Addr())
				}
				return struct{}{}
			}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
			outcome.ForEachOption(outcome.EmptyableToOption(stopping.Wait()), func(cause error) { t.Fatal(cause) })
			outcome.ForEachOption(outcome.EmptyableToOption(listening.tasks.Wait()), func(cause error) { t.Fatal(cause) })
		})
		return struct{}{}
	}, func(cause error) struct{} { t.Fatal(cause); return struct{}{} })
}
