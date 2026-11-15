package dispatcher

import (
	"os"
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
