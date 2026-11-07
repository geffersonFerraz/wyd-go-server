package game

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
	if err != nil || s != CppSettings() {
		t.Fatalf("full file: %+v err %v", s, err)
	}
	part := filepath.Join(dir, "part.yaml")
	os.WriteFile(part, []byte("channel: 3\nmob_ai:\n  min_attack_chance: 70\n"), 0o644)
	_, err = LoadSettings(part)
	if err == nil || !strings.Contains(err.Error(), "network") || !strings.Contains(err.Error(), "mob_ai.idle_think_seconds") {
		t.Fatalf("partial file: %v", err)
	}
}

func TestSettingsRejectsTyposAndBadValues(t *testing.T) {
	dir := t.TempDir()
	base := string(fullSettings(t))
	for name, body := range map[string]string{
		"typo":    base + "agro_while_walking: true\n",
		"channel": strings.Replace(base, "channel: 1", "channel: 0", 1),
		"half":    strings.Replace(base, "idle_think_seconds: 3", "idle_think_seconds: 0.3", 1),
		"chance":  strings.Replace(base, "min_attack_chance: 0", "min_attack_chance: 150", 1),
		"port":    strings.Replace(base, "listen_port: 8281", "listen_port: 0", 1),
		"ip":      strings.Replace(base, "listen_ip: 127.0.0.1", "listen_ip: localhost", 1),
	} {
		p := filepath.Join(dir, name+".yaml")
		os.WriteFile(p, []byte(body), 0o644)
		if _, err := LoadSettings(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
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
