package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/coreos/go-etcd/etcd"
	"github.com/deis/deis/pkg/aboutme"
	"github.com/deis/deis/pkg/etcd/discovery"
)

var version = "DEV"

func main() {
	log.Printf("Starting etcd-discovery boot version %s", version)

	ip, err := aboutme.MyIP()
	if err != nil {
		log.Printf("Failed to start because could not get IP: %s", err)
		os.Exit(321)
	}

	port := os.Getenv("DEIS_ETCD_CLIENT_PORT")
	if port == "" {
		port = "2381"
	}

	aurl := fmt.Sprintf("http://%s:%s", ip, port)
	curl := fmt.Sprintf("http://%s:%s,http://localhost:%s", ip, port, port)

	cmd := exec.Command("etcd", "-advertise-client-urls", aurl, "-listen-client-urls", curl)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	go func() {
		err := cmd.Start()
		if err != nil {
			log.Printf("Failed to start etcd: %s", err)
			os.Exit(2)
		}
	}()

	// Give etcd time to start up.
	log.Print("Etcd needs to start. Sleeping for 5 seconds...")
	time.Sleep(5 * time.Second)
	log.Print("Woke up.")

	uuid, err := discovery.Token()
	if err != nil {
		log.Printf("Failed to read %s", discovery.TokenFile)
		os.Exit(404)
	}
	size := os.Getenv("DEIS_ETCD_CLUSTER_SIZE")
	if size == "" {
		size = "3"
	}

	key := fmt.Sprintf(discovery.ClusterSizeKey, uuid)
	cli := etcd.NewClient([]string{"http://localhost:2381"})
	if _, err := cli.Create(key, size, 0); err != nil {
		log.Printf("Failed to add key: %s", err)
	}

	log.Printf("etcd-discovery service secret is %s.", key)
	log.Printf("etcd-discovery service is running on %s:%s.", ip, port)
	if err := cmd.Wait(); err != nil {
		log.Printf("Etcd stopped running: %s", err)
	}
}
