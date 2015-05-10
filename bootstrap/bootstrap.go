package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/flynn/flynn/bootstrap/discovery"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
)

type State struct {
	StepData   map[string]interface{}
	Providers  map[string]*ct.Provider
	Singleton  bool
	ClusterURL string
	Instances  []string
	Hosts      []cluster.Host

	controllerc   *controller.Client
	controllerKey string
}

func (s *State) ControllerClient() (*controller.Client, error) {
	if s.controllerc == nil {
		instances, err := discoverd.GetInstances("flynn-controller", time.Second)
		if err != nil {
			return nil, err
		}
		cc, err := controller.NewClient("http://"+instances[0].Addr, s.controllerKey)
		if err != nil {
			return nil, err
		}
		s.controllerc = cc
	}
	return s.controllerc, nil
}

func (s *State) SetControllerKey(key string) {
	s.controllerKey = key
}

type Action interface {
	Run(*State) error
}

var registeredActions = make(map[string]reflect.Type)

func Register(name string, action Action) {
	registeredActions[name] = reflect.Indirect(reflect.ValueOf(action)).Type()
}

type StepAction struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

type StepInfo struct {
	StepAction
	StepData  interface{} `json:"data,omitempty"`
	State     string      `json:"state"`
	Error     string      `json:"error,omitempty"`
	Err       error       `json:"-"`
	Timestamp time.Time   `json:"ts"`
}

var discoverdAttempts = attempt.Strategy{
	Min:   5,
	Total: 30 * time.Second,
	Delay: 200 * time.Millisecond,
}

func Run(manifest []byte, ch chan<- *StepInfo, minHosts int) (err error) {
	var a StepAction
	defer close(ch)
	defer func() {
		if err != nil {
			ch <- &StepInfo{StepAction: a, State: "error", Error: err.Error(), Err: err, Timestamp: time.Now().UTC()}
		}
	}()

	if minHosts == 2 {
		return errors.New("the minimum number of hosts for a multi-node cluster is 3, min-hosts=2 is invalid")
	}

	// Make sure we are connected to discoverd first
	discoverdAttempts.Run(func() error {
		return discoverd.DefaultClient.Ping()
	})

	steps := make([]json.RawMessage, 0)
	if err := json.Unmarshal(manifest, &steps); err != nil {
		return err
	}

	state := &State{
		StepData:  make(map[string]interface{}),
		Providers: make(map[string]*ct.Provider),
		Singleton: minHosts == 1,
	}
	if s := os.Getenv("SINGLETON"); s != "" {
		state.Singleton = s == "true"
	}

	a = StepAction{ID: "online-hosts", Action: "check"}
	ch <- &StepInfo{StepAction: a, State: "start", Timestamp: time.Now().UTC()}
	if err := checkOnlineHosts(minHosts, state); err != nil {
		return err
	}

	for _, s := range steps {
		if err := json.Unmarshal(s, &a); err != nil {
			return err
		}
		actionType, ok := registeredActions[a.Action]
		if !ok {
			return fmt.Errorf("bootstrap: unknown action %q", a.Action)
		}
		action := reflect.New(actionType).Interface().(Action)

		if err := json.Unmarshal(s, action); err != nil {
			return err
		}

		ch <- &StepInfo{StepAction: a, State: "start", Timestamp: time.Now().UTC()}

		if err := action.Run(state); err != nil {
			return err
		}

		si := &StepInfo{StepAction: a, State: "done", Timestamp: time.Now().UTC()}
		if data, ok := state.StepData[a.ID]; ok {
			si.StepData = data
		}
		ch <- si
	}

	return nil
}

var onlineHostAttempts = attempt.Strategy{
	Min:   5,
	Total: 5 * time.Second,
	Delay: 200 * time.Millisecond,
}

func checkOnlineHosts(count int, state *State) error {
	var online int

	timeout := time.After(30 * time.Second)
	for {
		// TODO: instance urls instead of url
		instances, err := discovery.GetCluster(state.ClusterURL)
		if err != nil {
			return fmt.Errorf("error discovering cluster: %s", err)
		}

		online = len(instances)
		if online >= count {
			state.Hosts = make([]cluster.Host, online)
			for i, inst := range instances {
				state.Hosts[i] = cluster.NewHostClient(inst.Name, inst.URL, nil)
			}
			// TODO: ping all instances
			break
		}

		select {
		case <-timeout:
			return fmt.Errorf("timed out waiting for %d hosts to come online (currently %d online)", count, online)
		default:
		}
	}
	return nil
}
