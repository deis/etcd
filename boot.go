package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/cookoo"
	"github.com/Masterminds/cookoo/log"
	"github.com/coreos/etcd/client"
	"github.com/deis/deis/pkg/aboutme"
	"github.com/deis/deis/pkg/env"
	"github.com/deis/deis/pkg/etcd"
	"github.com/deis/deis/pkg/etcd/discovery"
)

func main() {
	reg, router, c := cookoo.Cookoo()
	routes(reg)

	router.HandleRequest("boot", c, false)
}

func routes(reg *cookoo.Registry) {
	reg.AddRoute(cookoo.Route{
		Name: "boot",
		Help: "Boot Etcd",
		Does: []cookoo.Task{
			cookoo.Cmd{
				Name: "setenv",
				Fn:   iam,
			},
			cookoo.Cmd{
				Name: "discoveryToken",
				Fn:   discovery.GetToken,
			},
			// This synchronizes the local copy of env vars with the actual
			// environment. So all of these vars are available in cxt: or in
			// os.Getenv(). They will also be available to the etcd process
			// we spawn.
			cookoo.Cmd{
				Name: "vars",
				Fn:   env.Get,
				Using: []cookoo.Param{
					{Name: "DEIS_ETCD_DISCOVERY_SERVICE_HOST"},
					{Name: "DEIS_ETCD_DISCOVERY_SERVICE_PORT"},
					{Name: "DEIS_ETCD_1_SERVICE_HOST"},
					{Name: "DEIS_ETCD_1_SERVICE_PORT_CLIENT"},
					{Name: "DEIS_ETCD_CLUSTER_SIZE", DefaultValue: "3"},
					{Name: "DEIS_ETCD_DISCOVERY_TOKEN", From: "cxt:discoveryToken"},
					{Name: "HOSTNAME"},

					// Peer URLs are for traffic between etcd nodes.
					// These point to internal IP addresses, not service addresses.
					{Name: "ETCD_LISTEN_PEER_URLS", DefaultValue: "http://$MY_IP:$MY_PORT_PEER"},
					{Name: "ETCD_INITIAL_ADVERTISE_PEER_URLS", DefaultValue: "http://$MY_IP:$MY_PORT_PEER"},

					{
						Name:         "ETCD_LISTEN_CLIENT_URLS",
						DefaultValue: "http://$MY_IP:$MY_PORT_CLIENT,http://127.0.0.1:$MY_PORT_CLIENT",
					},
					{Name: "ETCD_ADVERTISE_CLIENT_URLS", DefaultValue: "http://$MY_IP:$MY_PORT_CLIENT"},

					// {Name: "ETCD_WAL_DIR", DefaultValue: "/var/"},
					// {Name: "ETCD_MAX_WALS", DefaultValue: "5"},
				},
			},

			// We need to connect to the discovery service to find out whether
			// we're part of a new cluster, or part of an existing cluster.
			cookoo.Cmd{
				Name: "discoveryClient",
				Fn:   etcd.CreateClient,
				Using: []cookoo.Param{
					{
						Name:         "url",
						DefaultValue: "http://$DEIS_ETCD_DISCOVERY_SERVICE_HOST:$DEIS_ETCD_DISCOVERY_SERVICE_PORT",
					},
				},
			},
			cookoo.Cmd{
				Name: "clusterClient",
				Fn:   etcd.CreateClient,
				Using: []cookoo.Param{
					{
						Name:         "url",
						DefaultValue: "http://$DEIS_ETCD_1_SERVICE_HOST:$DEIS_ETCD_1_SERVICE_PORT_CLIENT",
					},
				},
			},

			// If there is an existing cluster, set join mode to "existing",
			// Otherwise this gets set to "new". Note that we check the
			// 'status' directory on the discovery service. If the discovery
			// service is down, we will bail out, which will force a pod
			// restart.
			cookoo.Cmd{
				Name: "joinMode",
				Fn:   setJoinMode,
				Using: []cookoo.Param{
					{Name: "client", From: "cxt:discoveryClient"},
					{Name: "path", DefaultValue: "/deis/status/$DEIS_ETCD_DISCOVERY_TOKEN"},
					{Name: "desiredLen", From: "cxt:DEIS_ETCD_CLUSTER_SIZE"},
				},
			},
			// If joinMode is "new", we reroute to the route that creates new
			// clusters. Otherwise, we keep going on this chain, assuming
			// we're working with an existing cluster.
			cookoo.Cmd{
				Name: "rerouteIfNew",
				Fn: func(c cookoo.Context, p *cookoo.Params) (interface{}, cookoo.Interrupt) {
					if m := p.Get("joinMode", "").(string); m == "new" {
						return nil, cookoo.NewReroute("@newCluster")
					} else {
						log.Infof(c, "Join mode is %s", m)
					}
					return nil, nil
				},
				Using: []cookoo.Param{
					{Name: "joinMode", From: "cxt:joinMode"},
				},
			},

			// If we didn't get rerouted by the last command, we're in an
			// existing cluster, and we need to do some cleanup. First thing is
			// to remove any previous copies of this pod.
			cookoo.Cmd{
				Name: "removeMember",
				Fn:   etcd.RemoveMemberByName,
				Using: []cookoo.Param{
					{Name: "client", From: "cxt:clusterClient"},
					{Name: "name", From: "cxt:HOSTNAME"},
				},
			},

			// If any other etcd cluster members are gone (pod does not exist)
			// then we need to remove them from the cluster or else they will
			// remain voting members in the master election, and can eventually
			// deadlock the cluster.
			cookoo.Cmd{
				Name: "removeStale",
				Fn:   etcd.RemoveStaleMembers,
				Using: []cookoo.Param{
					{Name: "client", From: "cxt:clusterClient"},
					{Name: "namespace", From: "cxt:MY_NAMESPACE", DefaultValue: "default"},
					{Name: "label", DefaultValue: "name=deis-etcd-1"},
				},
			},

			// Now add self to cluster.
			cookoo.Cmd{
				Name: "addMember",
				Fn:   etcd.AddMember,
				Using: []cookoo.Param{
					{Name: "client", From: "cxt:clusterClient"},
					{Name: "name", From: "cxt:HOSTNAME"},
					{Name: "url", From: "cxt:ETCD_INITIAL_ADVERTISE_PEER_URLS"},
				},
			},

			// Find out who is in the cluster.
			cookoo.Cmd{
				Name: "initialCluster",
				Fn:   etcd.GetInitialCluster,
				Using: []cookoo.Param{
					{Name: "client", From: "cxt:clusterClient"},
				},
			},

			// Start etcd. Note that we use environment variables to pass
			// info into etcd, which is why there is so much env munging
			// in this startup script.
			cookoo.Cmd{
				Name: "startEtcd",
				Fn:   startEtcd,
				Using: []cookoo.Param{
					{Name: "client", From: "cxt:discoveryClient"},
					{Name: "discover", From: "cxt:discoveryUrl"},
				},
			},
		},
	})

	// This route gets called if boot gets part way through and discovers
	// that this is a new cluster. This short-circuits around the logic
	// that tries to detect a cluster and manage membership, and skips straight
	// to discovery via etcd-discovery.
	reg.AddRoute(cookoo.Route{
		Name: "@newCluster",
		Help: "Start as part of a new cluster",
		Does: []cookoo.Task{
			cookoo.Cmd{
				Name: "vars2",
				Fn:   env.Get,
				Using: []cookoo.Param{
					{
						Name:         "ETCD_DISCOVERY",
						DefaultValue: "http://$DEIS_ETCD_DISCOVERY_SERVICE_HOST:$DEIS_ETCD_DISCOVERY_SERVICE_PORT/v2/keys/deis/discovery/$DEIS_ETCD_DISCOVERY_TOKEN",
					},
				},
			},
			cookoo.Cmd{
				Name: "startEtcd",
				Fn:   startEtcd,
				Using: []cookoo.Param{
					{Name: "client", From: "cxt:discoveryClient"},
					{Name: "discover", From: "cxt:discoveryUrl"},
				},
			},
		},
	})
}

// setJoinMode determines what mode to start the etcd server in.
//
// In discovery mode, this will use the discovery URL to join a new cluster.
// In "existing" mode, this will join to an existing cluster directly.
//
// Params:
//	- client (etcd.Getter): initialized etcd client
//	- path (string): path to get. This will go through os.ExpandEnv().
// 	- desiredLen (string): The number of nodes to expect in etcd. This is
// 	usually stored as a string.
//
// Returns:
//  string "existing" or "new"
func setJoinMode(c cookoo.Context, p *cookoo.Params) (interface{}, cookoo.Interrupt) {
	cli := p.Get("client", nil).(client.Client)
	dlen := p.Get("desiredLen", "3").(string)
	path := p.Get("path", "").(string)
	path = os.ExpandEnv(path)

	state := "existing"

	dint, err := strconv.Atoi(dlen)
	if err != nil {
		log.Warnf(c, "Expected integer length, got '%s'. Defaulting to 3", dlen)
		dint = 3
	}

	// Ideally, this should look in the /deis/status directory in the discovery
	// service. That will indicate how many members have been online in the
	// last few hours. This is a good indicator of whether a cluster exists,
	// even if not all the hosts are healthy.
	res, err := etcd.SimpleGet(cli, path, true)
	if err != nil {
		return state, err
	}

	if !res.Node.Dir {
		return state, errors.New("Expected a directory node in discovery service")
	}

	if len(res.Node.Nodes) < dint {
		state = "new"
	}

	os.Setenv("ETCD_INITIAL_CLUSTER_STATE", state)
	return state, nil
}

// iam injects info into the environment about a host's self.
//
// Sets the following environment variables. (Values represent the data format.
// Instances will get its own values.)
//
//	MY_NODEIP=10.245.1.3
//	MY_SERVICE_IP=10.22.1.4
// 	MY_PORT_PEER=2380
// 	MY_PORT_CLIENT=2379
//	MY_NAMESPACE=default
//	MY_SELFLINK=/api/v1/namespaces/default/pods/deis-etcd-1-336jp
//	MY_UID=62a3b54a-6956-11e5-b8ab-0800279dd272
//	MY_APISERVER=https://10.247.0.1:443
//	MY_NAME=deis-etcd-1-336jp
//	MY_IP=10.246.44.7
//	MY_LABEL_NAME=deis-etcd-1   # One entry per label in the JSON
// 	MY_ANNOTATION_NAME=deis-etcd-1  # One entry per annitation in the JSON
//	MY_PORT_CLIENT=4100
//	MY_PORT_PEER=2380
func iam(c cookoo.Context, p *cookoo.Params) (interface{}, cookoo.Interrupt) {
	me, err := aboutme.FromEnv()
	if err != nil {
		log.Errf(c, "Failed aboutme.FromEnv: %s", err)
	} else {
		me.ShuntEnv()
		os.Setenv("ETCD_NAME", me.Name)
	}

	passEnv("MY_PORT_CLIENT", "$DEIS_ETCD_1_SERVICE_PORT_CLIENT")
	passEnv("MY_PORT_PEER", "$DEIS_ETCD_1_SERVICE_PORT_PEER")
	return nil, nil
}

func passEnv(newName, passthru string) {
	os.Setenv(newName, os.ExpandEnv(passthru))
}

// startEtcd starts a cluster member of a static etcd cluster.
//
// Params:
// 	- discover (string): Value to pass to etcd --discovery.
// 	- client (client.Client): A client to the discovery server. This will
// 	periodically write data there to indicate liveness.
func startEtcd(c cookoo.Context, p *cookoo.Params) (interface{}, cookoo.Interrupt) {
	cli := p.Get("client", nil).(client.Client)
	// Use config from environment.
	cmd := exec.Command("etcd")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	println(strings.Join(os.Environ(), "\n"))
	if err := cmd.Start(); err != nil {
		log.Errf(c, "Failed to start etcd: %s", err)
		return nil, err
	}

	// We need a way to tell starting members that there is an existing cluster,
	// and that that cluster has met the initial consensus requirements. This
	// basically stores a status record that indicates that it is an existing
	// and running etcd server. It allows for basically a two hour window
	// during which a cluster can be in an uncertain state before the entire
	// thing gives up and a new cluster is created.
	ticker := time.NewTicker(time.Minute)
	expires := time.Hour * 2
	go func() {
		name := c.Get("ETCD_NAME", "").(string)
		tok := c.Get("DEIS_ETCD_DISCOVERY_TOKEN", "").(string)
		key := fmt.Sprintf(discovery.ClusterStatusKey, tok, name)
		for t := range ticker.C {
			etcd.SimpleSet(cli, key, t.String(), expires)
		}
	}()

	if err := cmd.Wait(); err != nil {
		ticker.Stop()
		log.Errf(c, "Etcd quit unexpectedly: %s", err)
	}
	return nil, nil
}
