package cli

import (
	"fmt"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/bootstrap/discovery"
	"github.com/flynn/flynn/host/config"
)

func init() {
	Register("init", runInit, `
usage: flynn-host init [options]

options:
  --init-discovery    create and join a discovery token
  --discovery=TOKEN   join cluster with discovery token
  --peers=IPS         join cluster using seed IPs (must be already bootstrapped)
  --external=IP       external IP address of host, defaults to the first IPv4 address of eth0
  --file=NAME         file to write to [default: /etc/flynn/host.json]
  `)
}

func runInit(args *docopt.Args) error {
	c := config.New()

	discoveryToken := args.String["--discovery"]
	if args.Bool["--init-discovery"] {
		var err error
		discoveryToken, err = discovery.NewToken()
		if err != nil {
			return err
		}
		fmt.Println(discoveryToken)
	}
	if discoveryToken != "" {
		config.Args = append(config.Args, "--discovery", discoveryToken)
	}
	if ip := args.String["--external"]; ip != "" {
		config.Args = append(config.Args, "--external", ip)
	}
	if peers := args.String["--peers"]; peers != "" {
		config.Args = append(config.Args, "--peers", peers)
	}

	return c.WriteTo(args.String["--file"])
}
