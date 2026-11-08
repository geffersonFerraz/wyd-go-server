package cluster

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// docker-compose.yaml has one tmsrvN service per channel of tmsrv.yaml, with
// the same IP and port: DBSRV identifies channels by IP and serverlist.bin
// sends players to the port.
func TestComposeMatchesSettings(t *testing.T) {
	c, err := Load("../../tmsrv.yaml")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../docker-compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Command  []string `yaml:"command"`
			Networks map[string]struct {
				IPv4 string `yaml:"ipv4_address"`
			} `yaml:"networks"`
			Ports   []string `yaml:"ports"`
			Volumes []string `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatal(err)
	}
	var tmsrvs []string
	for name := range compose.Services {
		if strings.HasPrefix(name, "tmsrv") {
			tmsrvs = append(tmsrvs, name)
		}
	}
	if len(tmsrvs) != len(c.Channels) {
		t.Errorf("compose has %d tmsrv services %v, tmsrv.yaml %d channels", len(tmsrvs), tmsrvs, len(c.Channels))
	}
	for _, ch := range c.Channels {
		name := fmt.Sprintf("tmsrv%d", ch.Number)
		svc, ok := compose.Services[name]
		if !ok {
			t.Errorf("channel %d: no service %s in docker-compose.yaml", ch.Number, name)
			continue
		}
		if i := slices.Index(svc.Command, "-channel"); i < 0 || i+1 >= len(svc.Command) || svc.Command[i+1] != fmt.Sprint(ch.Number) {
			t.Errorf("%s: command %v, want -channel %d", name, svc.Command, ch.Number)
		}
		if ip := svc.Networks["wyd"].IPv4; ip != ch.IP {
			t.Errorf("%s: ipv4_address %s, tmsrv.yaml says %s", name, ip, ch.IP)
		}
		want := fmt.Sprintf("%d:%d", ch.Port, ch.Port)
		if !slices.Contains(svc.Ports, want) {
			t.Errorf("%s: ports %v, want %q", name, svc.Ports, want)
		}
		if !slices.Contains(svc.Volumes, "./tmsrv.yaml:/app/tmsrv.yaml:ro") {
			t.Errorf("%s: tmsrv.yaml not mounted (volumes %v)", name, svc.Volumes)
		}
	}
}
