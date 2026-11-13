package game

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"dbsrv/internal/cluster"
)

// The shipped tmsrv.yaml is the server's configuration: it must load, with
// every key present. Its values are whatever the owner chose.
func TestShippedSettingsLoad(t *testing.T) {
	if _, err := LoadSettings("../../tmsrv.yaml"); err != nil {
		t.Fatal(err)
	}
}

// fullSettings is a complete settings file (the C++ values).
func fullSettings(t *testing.T) []byte {
	t.Helper()
	raw, err := yaml.Marshal(CppSettings())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSettingsRequireFileAndEveryKey(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadSettings(filepath.Join(dir, "none.yaml")); err == nil {
		t.Fatal("missing file accepted")
	}
	p := filepath.Join(dir, "full.yaml")
	os.WriteFile(p, fullSettings(t), 0o644)
	s, err := LoadSettings(p)
	if err != nil || !reflect.DeepEqual(s, CppSettings()) {
		t.Fatalf("full file: %+v err %v", s, err)
	}
	part := filepath.Join(dir, "part.yaml")
	os.WriteFile(part, []byte("newbie_zone: 3\nmob_ai:\n  min_attack_chance: 70\n"), 0o644)
	_, err = LoadSettings(part)
	if err == nil || !strings.Contains(err.Error(), "servers") || !strings.Contains(err.Error(), "mob_ai.idle_think_seconds") {
		t.Fatalf("partial file: %v", err)
	}
}

func TestSettingsRejectsTyposAndBadValues(t *testing.T) {
	dir := t.TempDir()
	base := string(fullSettings(t))
	for name, body := range map[string]string{
		"typo":     base + "agro_while_walking: true\n",
		"channel":  strings.Replace(base, "number: 1", "number: 2", 1),
		"half":     strings.Replace(base, "idle_think_seconds: 3", "idle_think_seconds: 0.3", 1),
		"chance":   strings.Replace(base, "min_attack_chance: 0", "min_attack_chance: 150", 1),
		"port":     strings.Replace(base, "port: 8281", "port: 0", 1),
		"ip":       strings.ReplaceAll(base, "ip: 127.0.0.1\n", "ip: 0.0.0.0\n"),
		"public":   strings.Replace(base, "public_ip: 127.0.0.1", "public_ip: a_b.com", 1),
		"dbport":   strings.Replace(base, "port: 7514", "port: 0", 1),
		"cooldown": strings.Replace(base, "cooldown_seconds: 10", "cooldown_seconds: -1", 1),
	} {
		p := filepath.Join(dir, name+".yaml")
		os.WriteFile(p, []byte(body), 0o644)
		if _, err := LoadSettings(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestApplySettingsPicksChannel(t *testing.T) {
	s := CppSettings()
	s.Servers[0].Channels = append(s.Servers[0].Channels, cluster.Channel{Number: 2, IP: "127.0.0.2", Port: 8282})
	var c Config
	if err := c.ApplySettings(s, 0, 0); err == nil {
		t.Error("two channels and no -channel accepted")
	}
	if err := c.ApplySettings(s, 1, 3); err == nil {
		t.Error("channel 3 accepted")
	}
	if err := c.ApplySettings(s, 0, 2); err != nil {
		t.Fatal(err)
	}
	if c.Channel != 2 || c.ListenIP != "127.0.0.2" || c.ListenPort != 8282 || len(c.Cluster.Channels) != 2 {
		t.Errorf("config %+v", c)
	}
}

func TestAggroWhileWalkingOption(t *testing.T) {
	g := worldGame(t)
	id := hostileMob(t, g)
	m := &g.mobs[id]
	// Pretend the mob is walking to a route point far away.
	m.npc.curX, m.npc.curY = int(m.target.X)+40, int(m.target.Y)
	placePlayer(g, int(m.target.X)+1, int(m.target.Y)+1, 0)

	if r := g.standingBy(id); r&aiFoundEnemy != 0 {
		t.Fatal("C++ behavior: no aggro far from the route point")
	}
	g.cfg.MobAI.AggroWhileWalking = true
	m.npc.curX, m.npc.curY = int(m.target.X)+40, int(m.target.Y)
	if r := g.standingBy(id); r&aiFoundEnemy == 0 {
		t.Fatal("aggro_while_walking: player next to the mob not found")
	}
}
