package dispatcher_test

import (
	"strings"
	"testing"

	"github.com/solidtrace/factory/dispatcher"
)

func nomadEnv(changes map[string]string) func(string) string {
	env := map[string]string{
		dispatcher.EnvSolidTraceURL:   "https://console.example.com",
		dispatcher.EnvSolidTraceToken: "stk_x",
		dispatcher.EnvDatabase:        "/var/lib/dispatcher/d.sqlite3",
		dispatcher.EnvEncryptionKey:   "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		dispatcher.EnvNomadAddress:    "http://127.0.0.1:4646",
		dispatcher.EnvMachineKinds:    "standard=12288, large=28672",
		dispatcher.EnvWrapperURL:      "http://10.100.0.3:7600",
	}
	for k, v := range changes {
		env[k] = v
	}
	return func(k string) string { return env[k] }
}

func TestTheMachineKindsAreNodePoolsAndTheFirstIsTheDefault(t *testing.T) {
	c, err := dispatcher.LoadConfig("", nomadEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if k, ok := c.MachineKind(""); !ok || k != (dispatcher.MachineKind{Name: "standard", MemoryMB: 12288}) {
		t.Errorf("the default kind is %+v %v, want standard with 12288 MB", k, ok)
	}
	if k, ok := c.MachineKind("large"); !ok || k.MemoryMB != 28672 {
		t.Errorf("the kind large is %+v %v", k, ok)
	}
	if _, ok := c.MachineKind("huge"); ok {
		t.Error("a kind that the operator did not define was found")
	}
	if c.WrapperCommand != dispatcher.DefaultWrapperCommand {
		t.Errorf("the wrapper command is %q", c.WrapperCommand)
	}

	c, err = dispatcher.LoadConfig("", nomadEnv(map[string]string{dispatcher.EnvDefaultMachineKind: "large"}))
	if k, _ := c.MachineKind(""); err != nil || k.Name != "large" {
		t.Errorf("with the default kind large: %+v %v", k, err)
	}
}

func TestTheConfigurationNamesTheOneWrongNomadSetting(t *testing.T) {
	cases := map[string]map[string]string{
		dispatcher.EnvNomadAddress:       {dispatcher.EnvNomadAddress: "nomad:4646"},
		dispatcher.EnvMachineKinds:       {dispatcher.EnvMachineKinds: ""},
		dispatcher.EnvDefaultMachineKind: {dispatcher.EnvDefaultMachineKind: "huge"},
		dispatcher.EnvWrapperURL:         {dispatcher.EnvWrapperURL: ""},
		dispatcher.EnvWrapperCommand:     {dispatcher.EnvWrapperCommand: "factory-wrapper"},
	}
	for _, kinds := range []string{"standard", "standard=0", "standard=12 GB", "a pool=1024", "standard=1024,standard=2048"} {
		cases[dispatcher.EnvMachineKinds+" "+kinds] = map[string]string{dispatcher.EnvMachineKinds: kinds}
	}
	all := []string{dispatcher.EnvNomadAddress, dispatcher.EnvMachineKinds, dispatcher.EnvDefaultMachineKind,
		dispatcher.EnvWrapperURL, dispatcher.EnvWrapperCommand}
	for name, changes := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := dispatcher.LoadConfig("", nomadEnv(changes))
			want := strings.Fields(name)[0]
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("the error %v does not name %s", err, want)
			}
			for _, other := range all {
				if other != want && !strings.HasPrefix(other, want) && strings.Contains(err.Error(), other+" ") {
					t.Errorf("the error %q names %s, which is valid", err, other)
				}
			}
		})
	}
	// With no Nomad address there is no Runner, and the rest is not needed.
	if _, err := dispatcher.LoadConfig("", nomadEnv(map[string]string{dispatcher.EnvNomadAddress: "",
		dispatcher.EnvMachineKinds: "", dispatcher.EnvWrapperURL: ""})); err != nil {
		t.Errorf("with no Nomad address: %v", err)
	}
}
