package cluster

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"testing"

	"gopkg.in/yaml.v3"
)

// docker-compose.yaml runs some of the servers of tmsrv.yaml (the others are
// on other machines): for each of them it has the dbsrv service and one tmsrv
// service per channel, with the same IP and port. DBSRV identifies channels
// by IP and serverlist.bin sends players to the port. Services are matched by
// image and by their -server/-channel, not by name.
func TestComposeMatchesSettings(t *testing.T) {
	c, err := Load("../../tmsrv.yaml")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../docker-compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	type service struct {
		Image    string   `yaml:"image"`
		Command  []string `yaml:"command"`
		Networks map[string]struct {
			IPv4 string `yaml:"ipv4_address"`
		} `yaml:"networks"`
		Ports   []string `yaml:"ports"`
		Volumes []string `yaml:"volumes"`
	}
	var compose struct {
		Services map[string]service `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatal(err)
	}
	// flag is the number after name in the command (0 = not given).
	flag := func(svc service, name string) int {
		i := slices.Index(svc.Command, name)
		if i < 0 || i+1 >= len(svc.Command) {
			return 0
		}
		n, _ := strconv.Atoi(svc.Command[i+1])
		return n
	}
	// group is the server a service runs for: -server, or the only one.
	group := func(name string, svc service) int {
		g := flag(svc, "-server")
		if g == 0 {
			s, err := c.Server(0)
			if err != nil {
				t.Errorf("%s: no -server in the command: %v", name, err)
				return 0
			}
			g = s.Group
		}
		return g
	}
	type key struct{ group, channel int }
	tmsrvs := map[key]string{}
	dbsrvs := map[int]string{}
	for name, svc := range compose.Services {
		switch svc.Image {
		case "wyd-tmsrv":
			k := key{group(name, svc), flag(svc, "-channel")}
			if o, dup := tmsrvs[k]; dup {
				t.Errorf("%s and %s are both server %d channel %d", o, name, k.group, k.channel)
			}
			tmsrvs[k] = name
		case "wyd-dbsrv":
			g := group(name, svc)
			if o, dup := dbsrvs[g]; dup {
				t.Errorf("%s and %s are both the DBSRV of server %d", o, name, g)
			}
			dbsrvs[g] = name
		}
	}
	channels := 0
	for _, s := range c.Servers {
		name, ok := dbsrvs[s.Group]
		if !ok {
			continue // this server runs on another machine
		}
		channels += len(s.Channels)
		{
			svc := compose.Services[name]
			if ip := svc.Networks["wyd"].IPv4; ip != s.DBSRV.IP {
				t.Errorf("%s: ipv4_address %s, tmsrv.yaml says dbsrv.ip %s", name, ip, s.DBSRV.IP)
			}
			if slices.Contains(svc.Command, "-listen") {
				t.Errorf("%s: -listen in the command; the address comes from dbsrv in tmsrv.yaml", name)
			}
			if !slices.Contains(svc.Volumes, "./tmsrv.yaml:/app/tmsrv.yaml:ro") {
				t.Errorf("%s: tmsrv.yaml not mounted (volumes %v)", name, svc.Volumes)
			}
		}
		for _, ch := range s.Channels {
			name, ok := tmsrvs[key{s.Group, ch.Number}]
			if !ok {
				t.Errorf("server %d channel %d: no tmsrv service in docker-compose.yaml", s.Group, ch.Number)
				continue
			}
			svc := compose.Services[name]
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
	if len(tmsrvs) != channels {
		t.Errorf("compose has %d tmsrv services, the servers it runs have %d channels in tmsrv.yaml", len(tmsrvs), channels)
	}
	if len(dbsrvs) == 0 {
		t.Error("compose has no dbsrv service")
	}
	for g, name := range dbsrvs {
		if _, err := c.Server(g); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
