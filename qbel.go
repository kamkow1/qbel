package main

import (
	"flag"
	"log"
	"os"
	"syscall"
    "fmt"
	"os/exec"
	"path/filepath"
    "crypto/rand"
)

type Options struct {
    internal_command string
    internal_uuid string
    root string
    shell string
    mkimage_script string
}

var options Options
var uuid string
var qbelDir string

func init() {
    flag.StringVar(&options.root, "root", "", "Root FS mount path")
    flag.StringVar(&options.shell, "shell", "", "Path to shell app")
    flag.StringVar(&options.internal_command, "_ic", "create", "For internal usage")
    flag.StringVar(&options.internal_uuid, "_iuuid", "", "For internal usage")
    flag.StringVar(&options.mkimage_script, "mkimage", "", "Path to shell script which creates the image")
    flag.Parse()
    
    uuid = pseudo_uuid()
}

func pseudo_uuid() (uuid1 string) {

    b := make([]byte, 16)
    _, err := rand.Read(b)
    if err != nil {
        fmt.Println("Error: ", err)
        return
    }

    uuid1 = fmt.Sprintf("%04X-%04X-%04X-%04X-%04X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])

    return
}

func createQbel(root, shell, mkimage string) {
    log.Println("Creating container...")

    qbelDir = filepath.Join(root, "/.qbel-" + uuid)
    
    if err := os.MkdirAll(qbelDir, 0755); err != nil {
        log.Fatalln("could not create directories", err)
        os.Exit(1)
    }

    mkimage_path, err := filepath.Abs(mkimage)
    if err != nil {
        os.Exit(1)
    }

    exec.Command("cp", mkimage_path, filepath.Join(root, ".qbel-" + uuid)).Run()
    
    if err := runMkimageScript(filepath.Join(root, ".qbel-" + uuid, filepath.Base(mkimage)), root); err != nil {
        log.Fatalln("Could not run mkimage script", err)
        os.Exit(1)
    }
    
    args := []string{
        "-root=" + root,
        "-shell=" + shell,
        "-_ic=" + "shell",
        "-mkimage=" + mkimage,
        "-_iuuid=" + uuid,
    }
    cmd := exec.Command("/proc/self/exe", args...)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    var flags uintptr
    flags = syscall.CLONE_NEWNS  | syscall.CLONE_NEWUTS  |
            syscall.CLONE_NEWIPC | syscall.CLONE_NEWPID  |
            syscall.CLONE_NEWNET | syscall.CLONE_NEWUSER

    cmd.SysProcAttr = &syscall.SysProcAttr{
        Cloneflags: flags,
        UidMappings: []syscall.SysProcIDMap{
            { ContainerID: 0, HostID: os.Getuid(), Size: 1 },
        },
        GidMappings: []syscall.SysProcIDMap{
            { ContainerID: 0, HostID: os.Getuid(), Size: 1 },
        },
    }

    if err := cmd.Run(); err != nil {
        log.Fatalln("Error running container", err)
        os.Exit(1)
    }
}


func launchShell(root, shell, mkimage string) {
    qbelDir = filepath.Join(root, "/.qbel-" + uuid)

    log.Println("Launching shell", shell)
    
    err := syscall.Sethostname([]byte("qbel-"+uuid))
    if err != nil {
        log.Fatalln("Could not set hostname")
        os.Exit(1)
    }

    err = pivotRoot(qbelDir, mkimage)
    if err != nil {
        log.Fatalln("Could not pivot root", err)
        os.Exit(1)
    }

    cmd := exec.Command(shell)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    if err = cmd.Run(); err != nil {
        log.Fatalln("Running shell failed", err)
        os.Exit(1)
    }
    log.Println("Quitting shell")
}

func pivotRoot(root, mkimage string) error {
    if err := syscall.Mount(root, root, "", syscall.MS_BIND | syscall.MS_REC, ""); err != nil {
        log.Fatalln("Could not mount", err)
        return err
    }
    
    
    if err := syscall.PivotRoot(root, qbelDir); err != nil {
        log.Fatalln("syscall pivot_root failed", err)
        return err
    }

    if err := os.Chdir("/"); err != nil {
        log.Fatalln("Could not chdir", err)
        return err
    }
    
    if err := syscall.Unmount("/", syscall.MNT_DETACH); err != nil {
        log.Fatalln("Could not unmount", err)
        return err
    }

    // if err := os.RemoveAll("/"); err != nil {
    //     log.Fatalln("Could not remove directories", err)
    //     return err
    // }

    return nil
}

func runMkimageScript(script_path, root string) error {
    cmd := exec.Command(script_path)
    cmd.Dir = filepath.Join(root, ".qbel-" + uuid)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    return cmd.Run()
}

func main() {
    if options.root == ""  ||
       options.shell == "" ||
       options.mkimage_script == "" {
        flag.PrintDefaults()
        os.Exit(1)
    }

    if options.internal_uuid != "" {
        uuid = options.internal_uuid
    }

    if options.internal_command == "create" {
        createQbel(options.root, options.shell, options.mkimage_script)
        return
    }

    if options.internal_command == "shell" {
        launchShell(options.root, options.shell, options.mkimage_script)
        return
    }

    flag.PrintDefaults()
}
