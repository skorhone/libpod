// +build remote

package integration

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/containers/libpod/v2/pkg/rootless"
	"github.com/onsi/ginkgo"
)

func SkipIfRemote() {
	ginkgo.Skip("This function is not enabled for remote podman")
}

func SkipIfRootless() {
	if os.Geteuid() != 0 {
		ginkgo.Skip("This function is not enabled for rootless podman")
	}
}
func SkipIfRootlessV2() {
	if os.Geteuid() != 0 {
		ginkgo.Skip("This function is not enabled for v2 rootless podman")
	}
}

// Podman is the exec call to podman on the filesystem
func (p *PodmanTestIntegration) Podman(args []string) *PodmanSessionIntegration {
	var remoteArgs = []string{"--remote", "--url", p.RemoteSocket}
	remoteArgs = append(remoteArgs, args...)
	podmanSession := p.PodmanBase(remoteArgs, false, false)
	return &PodmanSessionIntegration{podmanSession}
}

// PodmanExtraFiles is the exec call to podman on the filesystem and passes down extra files
func (p *PodmanTestIntegration) PodmanExtraFiles(args []string, extraFiles []*os.File) *PodmanSessionIntegration {
	var remoteArgs = []string{"--remote", "--url", p.RemoteSocket}
	remoteArgs = append(remoteArgs, args...)
	podmanSession := p.PodmanAsUserBase(remoteArgs, 0, 0, "", nil, false, false, extraFiles)
	return &PodmanSessionIntegration{podmanSession}
}

// PodmanNoCache calls podman with out adding the imagecache
func (p *PodmanTestIntegration) PodmanNoCache(args []string) *PodmanSessionIntegration {
	var remoteArgs = []string{"--remote", "--url", p.RemoteSocket}
	remoteArgs = append(remoteArgs, args...)
	podmanSession := p.PodmanBase(remoteArgs, false, true)
	return &PodmanSessionIntegration{podmanSession}
}

// PodmanNoEvents calls the Podman command without an imagecache and without an
// events backend. It is used mostly for caching and uncaching images.
func (p *PodmanTestIntegration) PodmanNoEvents(args []string) *PodmanSessionIntegration {
	podmanSession := p.PodmanBase(args, true, true)
	return &PodmanSessionIntegration{podmanSession}
}

func (p *PodmanTestIntegration) setDefaultRegistriesConfigEnv() {
	defaultFile := filepath.Join(INTEGRATION_ROOT, "test/registries.conf")
	os.Setenv("REGISTRIES_CONFIG_PATH", defaultFile)
}

func (p *PodmanTestIntegration) setRegistriesConfigEnv(b []byte) {
	outfile := filepath.Join(p.TempDir, "registries.conf")
	os.Setenv("REGISTRIES_CONFIG_PATH", outfile)
	ioutil.WriteFile(outfile, b, 0644)
}

func resetRegistriesConfigEnv() {
	os.Setenv("REGISTRIES_CONFIG_PATH", "")
}
func PodmanTestCreate(tempDir string) *PodmanTestIntegration {
	pti := PodmanTestCreateUtil(tempDir, true)
	pti.StartRemoteService()
	return pti
}

func (p *PodmanTestIntegration) StartRemoteService() {
	if os.Geteuid() == 0 {
		os.MkdirAll("/run/podman", 0755)
	}

	args := []string{}
	if _, found := os.LookupEnv("DEBUG_SERVICE"); found {
		args = append(args, "--log-level", "debug")
	}
	remoteSocket := p.RemoteSocket
	args = append(args, "system", "service", "--time", "0", remoteSocket)
	podmanOptions := getRemoteOptions(p, args)
	command := exec.Command(p.PodmanBinary, podmanOptions...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	fmt.Printf("Running: %s %s\n", p.PodmanBinary, strings.Join(podmanOptions, " "))
	command.Start()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	p.RemoteCommand = command
	p.RemoteSession = command.Process
	err := p.DelayForService()
	p.RemoteStartErr = err
}

func (p *PodmanTestIntegration) StopRemoteService() {
	var out bytes.Buffer
	var pids []int
	remoteSession := p.RemoteSession

	if !rootless.IsRootless() {
		if err := remoteSession.Kill(); err != nil {
			fmt.Fprintf(os.Stderr, "error on remote stop-kill %q", err)
		}
		if _, err := remoteSession.Wait(); err != nil {
			fmt.Fprintf(os.Stderr, "error on remote stop-wait %q", err)
		}

	} else {
		//p.ResetVarlinkAddress()
		parentPid := fmt.Sprintf("%d", p.RemoteSession.Pid)
		pgrep := exec.Command("pgrep", "-P", parentPid)
		fmt.Printf("running: pgrep %s\n", parentPid)
		pgrep.Stdout = &out
		err := pgrep.Run()
		if err != nil {
			fmt.Fprint(os.Stderr, "unable to find remote pid")
		}

		for _, s := range strings.Split(out.String(), "\n") {
			if len(s) == 0 {
				continue
			}
			p, err := strconv.Atoi(s)
			if err != nil {
				fmt.Fprintf(os.Stderr, "unable to convert %s to int", s)
			}
			if p != 0 {
				pids = append(pids, p)
			}
		}

		pids = append(pids, p.RemoteSession.Pid)
		for _, pid := range pids {
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	socket := strings.Split(p.RemoteSocket, ":")[1]
	if err := os.Remove(socket); err != nil {
		fmt.Println(err)
	}
}

//MakeOptions assembles all the podman main options
func (p *PodmanTestIntegration) makeOptions(args []string, noEvents, noCache bool) []string {
	return args
}

//MakeOptions assembles all the podman main options
func getRemoteOptions(p *PodmanTestIntegration, args []string) []string {
	podmanOptions := strings.Split(fmt.Sprintf("--root %s --runroot %s --runtime %s --conmon %s --cni-config-dir %s --cgroup-manager %s",
		p.CrioRoot, p.RunRoot, p.OCIRuntime, p.ConmonBinary, p.CNIConfigDir, p.CgroupManager), " ")
	if os.Getenv("HOOK_OPTION") != "" {
		podmanOptions = append(podmanOptions, os.Getenv("HOOK_OPTION"))
	}
	podmanOptions = append(podmanOptions, strings.Split(p.StorageOptions, " ")...)
	podmanOptions = append(podmanOptions, args...)
	return podmanOptions
}

func (p *PodmanTestIntegration) RestoreArtifactToCache(image string) error {
	fmt.Printf("Restoring %s...\n", image)
	dest := strings.Split(image, "/")
	destName := fmt.Sprintf("/tmp/%s.tar", strings.Replace(strings.Join(strings.Split(dest[len(dest)-1], "/"), ""), ":", "-", -1))
	p.CrioRoot = p.ImageCacheDir
	restore := p.PodmanNoEvents([]string{"load", "-q", "-i", destName})
	restore.WaitWithDefaultTimeout()
	return nil
}

// SeedImages restores all the artifacts into the main store for remote tests
func (p *PodmanTestIntegration) SeedImages() error {
	return p.RestoreAllArtifacts()
}

// RestoreArtifact puts the cached image into our test store
func (p *PodmanTestIntegration) RestoreArtifact(image string) error {
	fmt.Printf("Restoring %s...\n", image)
	dest := strings.Split(image, "/")
	destName := fmt.Sprintf("/tmp/%s.tar", strings.Replace(strings.Join(strings.Split(dest[len(dest)-1], "/"), ""), ":", "-", -1))
	args := []string{"load", "-q", "-i", destName}
	podmanOptions := getRemoteOptions(p, args)
	command := exec.Command(p.PodmanBinary, podmanOptions...)
	fmt.Printf("Running: %s %s\n", p.PodmanBinary, strings.Join(podmanOptions, " "))
	command.Start()
	command.Wait()
	return nil
}

func (p *PodmanTestIntegration) DelayForService() error {
	for i := 0; i < 5; i++ {
		session := p.Podman([]string{"info"})
		session.WaitWithDefaultTimeout()
		if session.ExitCode() == 0 {
			return nil
		} else if i == 4 {
			break
		}
		time.Sleep(2 * time.Second)
	}
	return errors.New("Service not detected")
}

func populateCache(podman *PodmanTestIntegration) {}
func removeCache()                                {}
