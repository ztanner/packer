package plugin

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hashicorp/packer/packer-plugin-sdk/packer"
	pluginsdk "github.com/hashicorp/packer/packer-plugin-sdk/plugin"
	"github.com/hashicorp/packer/packer-plugin-sdk/tmp"
)

func newConfig() Config {
	var conf Config
	conf.PluginMinPort = 10000
	conf.PluginMaxPort = 25000
	return conf
}

func TestDiscoverReturnsIfMagicCookieSet(t *testing.T) {
	config := newConfig()

	os.Setenv(pluginsdk.MagicCookieKey, pluginsdk.MagicCookieValue)
	defer os.Unsetenv(pluginsdk.MagicCookieKey)

	err := config.Discover()
	if err != nil {
		t.Fatalf("Should not have errored: %s", err)
	}

	if len(config.builders) != 0 {
		t.Fatalf("Should not have tried to find builders")
	}
}

func TestEnvVarPackerPluginPath(t *testing.T) {
	// Create a temporary directory to store plugins in
	dir, _, cleanUpFunc, err := generateFakePlugins("custom_plugin_dir",
		[]string{"packer-provisioner-partyparrot"})
	if err != nil {
		t.Fatalf("Error creating fake custom plugins: %s", err)
	}

	defer cleanUpFunc()

	// Add temp dir to path.
	os.Setenv("PACKER_PLUGIN_PATH", dir)
	defer os.Unsetenv("PACKER_PLUGIN_PATH")

	config := newConfig()

	err = config.Discover()
	if err != nil {
		t.Fatalf("Should not have errored: %s", err)
	}

	if len(config.provisioners) == 0 {
		t.Fatalf("Should have found partyparrot provisioner")
	}
	if _, ok := config.provisioners["partyparrot"]; !ok {
		t.Fatalf("Should have found partyparrot provisioner.")
	}
}

func TestEnvVarPackerPluginPath_MultiplePaths(t *testing.T) {
	// Create a temporary directory to store plugins in
	dir, _, cleanUpFunc, err := generateFakePlugins("custom_plugin_dir",
		[]string{"packer-provisioner-partyparrot"})
	if err != nil {
		t.Fatalf("Error creating fake custom plugins: %s", err)
	}

	defer cleanUpFunc()

	pathsep := ":"
	if runtime.GOOS == "windows" {
		pathsep = ";"
	}

	// Create a second dir to look in that will be empty
	decoyDir, err := ioutil.TempDir("", "decoy")
	if err != nil {
		t.Fatalf("Failed to create a temporary test dir.")
	}
	defer os.Remove(decoyDir)

	pluginPath := dir + pathsep + decoyDir

	// Add temp dir to path.
	os.Setenv("PACKER_PLUGIN_PATH", pluginPath)
	defer os.Unsetenv("PACKER_PLUGIN_PATH")

	config := newConfig()

	err = config.Discover()
	if err != nil {
		t.Fatalf("Should not have errored: %s", err)
	}

	if len(config.provisioners) == 0 {
		t.Fatalf("Should have found partyparrot provisioner")
	}
	if _, ok := config.provisioners["partyparrot"]; !ok {
		t.Fatalf("Should have found partyparrot provisioner.")
	}
}

func generateFakePlugins(dirname string, pluginNames []string) (string, []string, func(), error) {
	dir, err := ioutil.TempDir("", dirname)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to create temporary test directory: %v", err)
	}

	cleanUpFunc := func() {
		os.RemoveAll(dir)
	}

	var suffix string
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}

	plugins := make([]string, len(pluginNames))
	for i, plugin := range pluginNames {
		plug := filepath.Join(dir, plugin+suffix)
		plugins[i] = plug
		_, err := os.Create(plug)
		if err != nil {
			cleanUpFunc()
			return "", nil, nil, fmt.Errorf("failed to create temporary plugin file (%s): %v", plug, err)
		}
	}

	return dir, plugins, cleanUpFunc, nil
}

// TestHelperProcess isn't a real test. It's used as a helper process
// for multiplugin-binary tests.
func TestHelperPlugins(*testing.T) {
	if os.Getenv("PKR_WANT_TEST_PLUGINS") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No command\n")
		os.Exit(2)
	}

	pluginName, args := args[0], args[1:]
	plugin, found := mockPlugins[pluginName]
	if !found {
		fmt.Fprintf(os.Stderr, "No %q plugin found\n", pluginName)
		os.Exit(2)
	}

	plugin.RunCommand(args...)
}

// HasExec reports whether the current system can start new processes
// using os.StartProcess or (more commonly) exec.Command.
func HasExec() bool {
	switch runtime.GOOS {
	case "js":
		return false
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return false
		}
	}
	return true
}

// MustHaveExec checks that the current system can start new processes
// using os.StartProcess or (more commonly) exec.Command.
// If not, MustHaveExec calls t.Skip with an explanation.
func MustHaveExec(t testing.TB) {
	if !HasExec() {
		t.Skipf("skipping test: cannot exec subprocess on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func MustHaveCommand(t testing.TB, cmd string) string {
	path, err := exec.LookPath(cmd)
	if err != nil {
		t.Skipf("skipping test: cannot find the %q command: %v", cmd, err)
	}
	return path
}

func helperCommand(t *testing.T, s ...string) []string {
	MustHaveExec(t)

	cmd := []string{os.Args[0], "-test.run=TestHelperPlugins", "--"}
	return append(cmd, s...)
}

var (
	mockPlugins = map[string]pluginsdk.Set{
		"bird": pluginsdk.Set{
			Builders: map[string]packer.Builder{
				"feather":   nil,
				"guacamole": nil,
			},
		},
		"chimney": pluginsdk.Set{
			PostProcessors: map[string]packer.PostProcessor{
				"smoke": nil,
			},
		},
	}
)

func Test_multiplugin_describe(t *testing.T) {

	pluginDir, err := tmp.Dir("pkr-multiplugin-test-*")
	{
		// create fake plugins that are started through SH
		// they actually start TestHelperPlugins which in turn allow to call
		// describe upon plugins
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(pluginDir)

		t.Logf("working in %s", pluginDir)
		defer os.RemoveAll(pluginDir)

		shPath := MustHaveCommand(t, "sh")
		for name := range mockPlugins {
			plugin := path.Join(pluginDir, "packer-plugin-"+name)
			fileContent := fmt.Sprintf("#!%s\n", shPath)
			fileContent += strings.Join(
				append([]string{"PKR_WANT_TEST_PLUGINS=1"}, helperCommand(t, name, "$@")...),
				" ")
			ioutil.WriteFile(plugin, []byte(fileContent), os.ModePerm)
		}
	}
	os.Setenv("PACKER_PLUGIN_PATH", pluginDir)

	c := Config{}
	err = c.Discover()
	if err != nil {
		t.Fatal(err)
	}

	for mockPluginName, plugin := range mockPlugins {
		for mockBuilderName := range plugin.Builders {
			expectedBuilderName := mockPluginName + "-" + mockBuilderName
			if _, found := c.builders[expectedBuilderName]; !found {
				t.Fatalf("expected to find builder %q", expectedBuilderName)
			}
		}
		for mockProvisionerName := range plugin.Provisioners {
			expectedProvisionerName := mockPluginName + "-" + mockProvisionerName
			if _, found := c.provisioners[expectedProvisionerName]; !found {
				t.Fatalf("expected to find builder %q", expectedProvisionerName)
			}
		}
		for mockPostProcessorName := range plugin.PostProcessors {
			expectedPostProcessorName := mockPluginName + "-" + mockPostProcessorName
			if _, found := c.postProcessors[expectedPostProcessorName]; !found {
				t.Fatalf("expected to find post-processor %q", expectedPostProcessorName)
			}
		}
	}
}
