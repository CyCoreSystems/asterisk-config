package template

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/CyCoreSystems/netdiscover/discover"
	"github.com/ericchiang/k8s"
	"github.com/ericchiang/k8s/apis/core/v1"
)

// KubeAPITimeout is the amount of time to wait for the kubernetes API to respond before failing
var KubeAPITimeout = 10 * time.Second

func init() {
	rand.Seed(time.Now().Unix())
}

// Engine provides the template rendering engine
type Engine struct {
	kc *k8s.Client

	disc discover.Discoverer

	reload chan error

	firstRenderCompleted bool

	watchers map[string]*k8s.Watcher

	AMISecret string

	mu sync.Mutex
}

// NewEngine returns a new rendering engine.  The supplied reloadChan servces
// as an indicator to reload and as a return channel for errors.  If `nil` is
// passed down the channel, a reload is requested.  If an error is passed down,
// the Engine has died and must be restarted.
func NewEngine(reloadChan chan error, disc discover.Discoverer, amiSecret string) *Engine {
	return &Engine{
		AMISecret: amiSecret,
		disc:      disc,
		reload:    reloadChan,
		watchers:  make(map[string]*k8s.Watcher),
	}
}

// Close shuts down the template engine
func (e *Engine) Close() {

	e.mu.Lock()
	for _, w := range e.watchers {
		w.Close() // nolint
	}
	e.watchers = nil
	e.mu.Unlock()

}

func (e *Engine) connectKube() (err error) {
	if e.kc != nil {
		return
	}

	e.kc, err = k8s.NewInClusterClient()

	return
}

func (e *Engine) failure(err error) {
	e.Close()

	if e.reload != nil {
		e.reload <- err
	}
}

// FirstRenderComplete should be called after the first render is completed.
// This eliminates some extra kubernetes API checking for resource watching.
func (e *Engine) FirstRenderComplete(ok bool) {
	e.firstRenderCompleted = ok
}

// ConfigMap returns a kubernetes ConfigMap
func (e *Engine) ConfigMap(name string, namespace string) (c *v1.ConfigMap, err error) {
	c = new(v1.ConfigMap)

	if err = e.connectKube(); err != nil {
		return
	}

	namespace, err = getNamespace(namespace)
	if err != nil {
		return
	}

	ctx, cancel := boundedContext()
	defer cancel()

	if err = e.kc.Get(ctx, namespace, name, c); err != nil {
		return
	}

	if e.firstRenderCompleted {
		return
	}

	// If this is the first run, register a watcher
	wName := watcherName("configmap", namespace)

	// start one watcher for each kind + namespace
	if _, ok := e.watchers[wName]; !ok {

		var watched = new(v1.ConfigMap)

		watcher, err := e.kc.Watch(context.Background(), namespace, watched)

		e.watchers[wName] = watcher

		go func() {
			for {
				if _, err = watcher.Next(watched); err != nil {
					e.failure(err)
					return
				}
				e.reload <- nil
			}
		}()
	}

	return
}

// Env returns the value of an environment variable
func (e *Engine) Env(name string) string {
	return os.Getenv(name)
}

// Service returns a kubernetes Service
func (e *Engine) Service(name, namespace string) (s *v1.Service, err error) {
	s = new(v1.Service)

	if err = e.connectKube(); err != nil {
		return
	}

	namespace, err = getNamespace(namespace)
	if err != nil {
		return
	}

	ctx, cancel := boundedContext()
	defer cancel()

	err = e.kc.Get(ctx, namespace, name, s)

	if e.firstRenderCompleted {
		return
	}

	// If this is the first run, register a watcher
	wName := watcherName("service", namespace)

	// start one watcher for each kind + namespace
	if _, ok := e.watchers[wName]; !ok {

		var watched = new(v1.Service)

		watcher, err := e.kc.Watch(context.Background(), namespace, watched)

		e.watchers[wName] = watcher

		go func() {
			for {
				if _, err = watcher.Next(watched); err != nil {
					e.failure(err)
					return
				}
				e.reload <- nil
			}
		}()
	}

	return

}

// Endpoints returns the Endpoints for the given Service
func (e *Engine) Endpoints(name, namespace string) (ep *v1.Endpoints, err error) {
	ep = new(v1.Endpoints)

	if err = e.connectKube(); err != nil {
		return
	}

	namespace, err = getNamespace(namespace)
	if err != nil {
		return
	}

	ctx, cancel := boundedContext()
	defer cancel()

	err = e.kc.Get(ctx, namespace, name, ep)

	if e.firstRenderCompleted {
		return
	}

	// If this is the first run, register a watcher
	wName := watcherName("endpoints", namespace)

	// start one watcher for each kind + namespace
	if _, ok := e.watchers[wName]; !ok {

		var watched = new(v1.Endpoints)

		watcher, err := e.kc.Watch(context.Background(), namespace, watched)

		e.watchers[wName] = watcher

		go func() {
			for {
				if _, err = watcher.Next(watched); err != nil {
					e.failure(err)
					return
				}
				e.reload <- nil
			}
		}()
	}

	return
}

// EndpointIPs returns the set of IP addresses for the given Service's endpoints.
func (e *Engine) EndpointIPs(name, namespace string) (out []string, err error) {
	var ep *v1.Endpoints

	ep, err = e.Endpoints(name, namespace)
	if err != nil {
		return
	}

	for _, ss := range ep.GetSubsets() {
		for _, addr := range ss.GetAddresses() {
			out = append(out, addr.GetIp())
		}
	}
	return

}

// Network retrieves network information about the running Pod.
func (e *Engine) Network(kind string) (string, error) {

	kind = strings.ToLower(kind)
	switch kind {
	case "hostname":
		return e.disc.Hostname()
	case "privateipv4":
		ip, err := e.disc.PrivateIPv4()
		return ip.String(), err
	case "privatev4":
		ip, err := e.disc.PrivateIPv4()
		return ip.String(), err
	case "publicipv4":
		ip, err := e.disc.PublicIPv4()
		return ip.String(), err
	case "publicv4":
		ip, err := e.disc.PublicIPv4()
		return ip.String(), err
	case "publicipv6":
		ip, err := e.disc.PublicIPv6()
		return ip.String(), err
	case "publicv6":
		ip, err := e.disc.PublicIPv6()
		return ip.String(), err
	default:
		return "", errors.New("unhandled kind")
	}
}

func boundedContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), KubeAPITimeout)
}

func getNamespace(name string) (out string, err error) {
	out = name
	if out == "" {
		out = os.Getenv("POD_NAMESPACE")
	}
	if out == "" {
		err = errors.New("failed to determine namespace")
	}
	return
}

func watcherName(names ...string) string {
	return strings.Join(names, ".")
}
