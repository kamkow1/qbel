package main

import (
	"io/ioutil"
	"os"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func logFatalfAndQuit(fmt string, args ...any) {
	log.Fatalf(fmt, args...)
	os.Exit(1)
}

var (
	// ContainerRootfsPath is a path to the root of the filesystem (ie. `/`)
	// that will be seen from the perspective of the container.
	ContainerRootfsPath = ""

	// OldRootPath : Path that old rootfilesystem to be moved in container.
	OldRootPath = ".old_root"

	// ContainerHostName : The name to be used as hostname in container
	ContainerHostName = ""

    Application = []string{}
)

func init() {
    script_dir, ok := os.LookupEnv("QBEL_SCRIPTS")
    if !ok {
        logFatalfAndQuit("QBEL_SCRIPTS not set\n")
    }

    cwd, _ := os.Getwd()
    qbelfile_path := filepath.Join(cwd, "Qbelfile")
    bytes, err := ioutil.ReadFile(qbelfile_path)
    if err != nil {
		logFatalfAndQuit("Could not read Qbelfile: %v\n", err)
    }

    qbelfile := string(bytes)

    lines := strings.Split(qbelfile, "\n")
    for _, line := range lines {
        fields := strings.Fields(line)
        if len(fields) == 0 {
            continue
        }

        if fields[0][0] == '#' {
            continue
        }
        
        if fields[0] == "CONTAINER" {
            ContainerHostName = fields[1]
            
            rootPrePath, _ := filepath.Abs(fields[2])
            ContainerRootfsPath = filepath.Join(rootPrePath, ContainerHostName + "-rootfs")
            os.MkdirAll(ContainerRootfsPath, 0755)
        } else if fields[0] == "RUN" {
            _, ok := os.LookupEnv("QBEL_SETUPDONE")
            if ok {
                continue
            }
            if _, err := os.Stat(filepath.Join(ContainerRootfsPath, ".SETUP")); err == nil {
                continue
            }

            cmd := exec.Command(filepath.Join(script_dir, fields[1]), fields[2:]...)
            cmd.Dir = ContainerRootfsPath
            cmd.Stdin = os.Stdin
            cmd.Stdout = os.Stdout
            cmd.Stderr = os.Stderr
            if err := cmd.Run(); err != nil {
				logFatalfAndQuit("Command %v failed: %v\n", cmd.Args, err)
            }
        } else if fields[0] == "APP" {
            Application = fields[1:]
        }
    }

	if ContainerHostName == "" {
		logFatalfAndQuit("Container hostname is not set!\n")
	}

	if len(Application) == 0 {
		logFatalfAndQuit("No application commandline provided\n")
	}

	if ContainerRootfsPath == "" {
		logFatalfAndQuit("Container root fs is not set\n")
	}

    os.Setenv("QBEL_SETUPDONE", "Y")
    if err := os.WriteFile(filepath.Join(ContainerRootfsPath, ".SETUP"), []byte("Y"), 0666); err != nil {
		logFatalfAndQuit("Could not write .SETUP: %v\n", err)
    }
}

func main() {
	switch os.Args[1] {
	case "run":
		selfExec()
	case "spawner":
		spawner()
	default:
		logFatalfAndQuit("Unknown command: %s\n", os.Args[1])
	}
}

// Set the hostname of container
func setHostName(hostName string) {
	must(syscall.Sethostname([]byte(hostName)))
}

// Make necessary mounts for container such as /proc.
func mounts(newRoot string) {
	procDict := filepath.Join(newRoot, "/proc")
	_ = os.Mkdir(procDict, 0777)
	must(syscall.Mount(newRoot, procDict, "proc", uintptr(0), ""))
	
    sysDict := filepath.Join(newRoot, "/sys")
	_ = os.Mkdir(sysDict, 0777)
	must(syscall.Mount(newRoot, sysDict, "sysfs", uintptr(0), ""))
    
    tmpDict := filepath.Join(newRoot, "/tmp")
	_ = os.Mkdir(tmpDict, 0777)
	must(syscall.Mount(newRoot, tmpDict, "tmpfs", uintptr(0), ""))
}

// Prepare root fielsystem for container.
func prepareRootfs(newRoot string) {
	/*
		From Linux man:
		int pivot_root(const char *new_root, const char *put_old);
			...
			pivot_root() moves the root file system of the calling process to the directory put_old and makes
			new_root the new root file system of the calling process.

			...
				The following restrictions apply to new_root and put_old:
				-  They must be directories.
				-  new_root and put_old must not be on the same filesystem as the current root.
				-  put_old must be underneath new_root, that is, adding a nonzero number
					of /.. to the string pointed to by put_old must yield the same directory as new_root.
				-  No other filesystem may be mounted on put_old.
	*/

	// Since `new_root and put_old must not be on the same filesystem as the current root.`, we need to mount newroot.
	must(syscall.Mount(newRoot, newRoot, "", syscall.MS_BIND|syscall.MS_REC, ""))

	// Create `{newRoot}/.oldroot` for old root filesystem.
	putOld := filepath.Join(newRoot, OldRootPath)
	_ = os.Mkdir(putOld, 0777)

	// This is related to the systemd mounts. We should change the roto mount to private before pivotting.
	must(syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""))

	// Move the root filesystem to new filesystem
	must(syscall.PivotRoot(newRoot, putOld))

	// Change working directory to /
	must(syscall.Chdir("/"))

	// Unmount old filesystem which is now under /.old_root
	must(syscall.Unmount(OldRootPath, syscall.MNT_DETACH))

	// Remove temporary old filesystem
	must(os.RemoveAll(OldRootPath))
}

func selfExec() {
	// For some security reasons we should set hostname before running the actual container program. So we need a middle process
	// that is cloned with new namespaces and this process should change hostname, prepare filesystem and then exec the container program.
	cmd := exec.Command("/proc/self/exe", append([]string{"spawner"}, os.Args[2:]...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWUSER |
            syscall.CLONE_NEWIPC |
            syscall.CLONE_NEWNET |
            syscall.CLONE_NEWCGROUP |
            syscall.CLONE_NEWTIME,
		UidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      os.Getuid(),
				Size:        1,
			},
		},
		GidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      os.Getgid(),
				Size:        1,
			},
		},
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		logFatalfAndQuit("Command %v failed: %v\n", cmd.Args, err)
	}
}

func spawner() {
	mounts(ContainerRootfsPath)
	prepareRootfs(ContainerRootfsPath)
	setHostName(ContainerHostName)

    cmd := exec.Command(Application[0], Application[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		logFatalfAndQuit("Command %v failed: %v\n", cmd.Args, err)
		os.Exit(1)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
