package main

import (
	"flag"
	"fmt"
	docker "github.com/docker/engine-api/client"
	dt "github.com/docker/engine-api/types"
	"github.com/hpcloud/tail"
	"golang.org/x/net/context"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	METRIC_NAME        = "oom_totals"
	METRIC_DESCRIPTION = "OOM counts per container"
	VERSION            = "0.1.10"
)

var (
	oomCounter   = make(map[string]int)
	input        = flag.String("input", "/var/log/kern.log", "Input file")
	output       = flag.String("output", "/tmp/cacca.prom", "Output file")
	printVersion = flag.Bool("version", false, "Print version number and quit")
	dockerClient *docker.Client
	reCName      = regexp.MustCompile("[a-zA-Z-]+-[0-9]+-([a-zA-Z-]+)-[a-f0-9]{20}")
)

func write() {
	log.Println("Write")

	// write oomCounter to tmp file
	tmpfile, err := ioutil.TempFile("", "prom_oom")

	if err != nil {
		log.Fatalln("Cannot open temporary file")
	}

	fmt.Fprintf(tmpfile, "# HELP %s %s\n", METRIC_NAME, METRIC_DESCRIPTION)
	fmt.Fprintf(tmpfile, "# TYPE %s\n", METRIC_NAME)

	for k := range oomCounter {
		fmt.Fprintf(tmpfile, "%s{container=\"%s\"} %d\n", METRIC_NAME, k, oomCounter[k])
	}
	if err = tmpfile.Close(); err != nil {
		log.Fatalln("Cannot close temporary file: " + err.Error())
	}

	// atomic mv tmp file to output dir
	if err = os.Rename(tmpfile.Name(), *output); err != nil {
		log.Fatalln("Cannot write to output file: " + err.Error())
	}
}

func containerName(id string) string {
	container, err := dockerClient.ContainerInspect(context.Background(), id)

	if err != nil {
		log.Printf("Error getting information about container %s (skipping): %e\n", id, err.Error())
	} else {
		name := container.Name
		match := reCName.FindStringSubmatch(name)
		if len(match) < 2 {
			log.Println("Container name does not match pattern")
		} else {
			return match[1]
		}
	}
	return ""
}

// prometheus doesn't like labels to appear out of the blue, rate() doesn't work. Here we periodically fetch containers
// and set oomCounter accordingly
func setZeros() {
	for {
		options := dt.ContainerListOptions{}
		cs, err := dockerClient.ContainerList(context.Background(), options)
		if err != nil {
			log.Println("Can not connect to docker")
		} else {
			for _, c := range cs {
				name := containerName(c.ID)
				if name != "" {
					if oomCounter[name] == 0 {
						oomCounter[name] = 0
					}
				}
			}
		}
		write()

		time.Sleep(10 * time.Second)
	}
}

func init() {
	var err error

	defaultHeaders := map[string]string{"User-Agent": "engine-api-cli-1.0"}
	dockerClient, err = docker.NewClient("unix:///var/run/docker.sock", "v1.21", nil, defaultHeaders)

	if err != nil {
		log.Fatalln("Can't connect to docker socket")
	}

}

func main() {
	flag.Parse()

	if *printVersion {
		fmt.Println("Version: " + VERSION)
		os.Exit(0)
	}

	go setZeros()

	t, err := tail.TailFile(*input, tail.Config{Follow: true})
	if err != nil {
		log.Fatalln("Log file does not exist")
	}
	re := regexp.MustCompile(".*cpuset=(.{12})")

	for line := range t.Lines {
		if strings.Contains(line.Text, "invoked oom-killer") {

			// get next line, it contains the container ID
			l := <-t.Lines

			// container ID
			cid := re.FindStringSubmatch(l.Text)[1]

			name := containerName(cid)

			if name != "" {
				oomCounter[name] += 1
			}

			write()
		}
	}
}
