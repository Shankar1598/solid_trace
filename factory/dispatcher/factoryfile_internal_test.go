package dispatcher

import (
	"os"
	"strings"
	"testing"
)

// The reference configuration's example is a valid factory file.
func TestTheExampleFactoryFileIsValid(t *testing.T) {
	body, err := os.ReadFile("../reference/factory.toml")
	if err != nil {
		t.Fatal(err)
	}
	d := &Dispatcher{config: Config{MachineKinds: []MachineKind{{Name: "standard", MemoryMB: 12288}}}}
	f, problem := d.parseFactoryFile(body)
	if problem != "" {
		t.Fatalf("the example: %s", problem)
	}
	if f.MachineKind != "standard" || f.Setup == "" || len(f.Checks) != 2 || len(f.AllowedHosts) != 2 ||
		f.Limits.IdleSeconds != 45*60 || f.Limits.TotalSeconds != 12*3600 || len(f.SaveExclude) != 2 {
		t.Errorf("the example gives %+v", f)
	}
}

// limits.idle and limits.total take 1 minute or more, in whole seconds.
func TestTheLimitsOfAFactoryFileAreOneMinuteOrMoreInWholeSeconds(t *testing.T) {
	d := &Dispatcher{}
	for _, c := range []struct {
		value   string
		seconds int64
	}{{"1m", 60}, {"60s", 60}, {"90s", 90}, {"59s", 0}, {"90500ms", 0}, {"0s", 0}, {"-5m", 0}, {"soon", 0}} {
		for _, name := range []string{"idle", "total"} {
			f, problem := d.parseFactoryFile([]byte("[limits]\n" + name + " = \"" + c.value + "\"\n"))
			got := f.Limits.IdleSeconds
			if name == "total" {
				got = f.Limits.TotalSeconds
			}
			if c.seconds == 0 {
				if problem == "" || !strings.Contains(problem, "limits."+name) {
					t.Errorf("%s = %q: the problem is %q, want a refusal that names limits.%s", name, c.value, problem, name)
				}
				continue
			}
			if problem != "" || got != c.seconds {
				t.Errorf("%s = %q: %d seconds, problem %q, want %d seconds", name, c.value, got, problem, c.seconds)
			}
		}
	}
}
