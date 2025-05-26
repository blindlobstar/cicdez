package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/getsops/sops/v3/decrypt"
	"gopkg.in/yaml.v3"
)

const (
	GitTag = "git_tag"
	GitSha = "git_sha"

	CmdPre    = "pre"
	CmdBuild  = "build"
	CmdDeploy = "deploy"
)

type Config struct {
	Things map[string]Thing
}

type Thing struct {
	Pre    []string
	Build  Build
	Deploy Deploy
}

type Build struct {
	Image    string
	File     string
	Context  string
	Tags     []string
	Platform string
}

type Deploy struct {
	Name    string
	File    string
	Context string
	Auth    string
	Env     map[string]string
}

func main() {
	flag.Parse()
	if len(flag.Args()) < 1 {
		// TODO: print usage
		os.Exit(1)
	}
	tname := flag.Arg(0)

	cfgB, err := os.ReadFile("cicdez.yaml")
	if err != nil {
		log.Fatalf("can't open cicdez.yaml: %s", err.Error())
	}
	var cfg Config
	if err := yaml.Unmarshal(cfgB, &cfg); err != nil {
		log.Fatalf("can't read cicdez.yaml: %s", err.Error())
	}

	t, ok := cfg.Things[tname]
	if !ok {
		log.Fatalf("thing: %s is not presented", tname)
	}

	tmp, err := os.MkdirTemp("", "cicdez-*")
	if err != nil {
		log.Fatalf("can't created temp dir: %s", err.Error())
	}
	defer os.RemoveAll(tmp)

	branch, err := outCommand("", "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		log.Fatal(err, "error getting current branch name")
	}

	if err := runCommand("", "git", "clone", "--single-branch", "--branch", branch, ".", tmp); err != nil {
		log.Fatalf("error running git clone command: %s", err)
	}

	secrets := map[string]any{}
	if sf := os.Getenv("SECRET_FILE"); sf != "" {
		secretData, err := decrypt.File(sf, "yaml")
		if err != nil {
			log.Fatalf("can't decrypt secret file: %s", err.Error())
		}

		if err := yaml.Unmarshal(secretData, &secrets); err != nil {
			log.Fatalf("can't unmarshal secret file: %s", err.Error())
		}

		for key, value := range secrets {
			if strValue, ok := value.(string); ok {
				os.Setenv(key, strValue)
			}
		}
	}

	if len(flag.Args()) < 2 {
		if err := pre(tmp, t.Pre); err != nil {
			log.Fatal(err)
		}
		if err := build(tmp, t.Build); err != nil {
			log.Fatal(err)
		}
		if err := deploy(tmp, t.Deploy); err != nil {
			log.Fatal(err)
		}
	}

	switch flag.Arg(1) {
	case CmdPre:
		err = pre(tmp, t.Pre)
	case CmdBuild:
		err = build(tmp, t.Build)
	case CmdDeploy:
		err = deploy(tmp, t.Deploy)
	default:
		log.Fatalf("not supported command: %s", flag.Arg(2))
	}
	if err != nil {
		log.Fatal(err)
	}
}

func pre(tmpdir string, instructions []string) error {
	for _, pre := range instructions {
		args := strings.Split(pre, " ")
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Dir = tmpdir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("error running %s: %w", pre, err)
		}
	}

	return nil
}

func build(tmpdir string, config Build) error {
	bargs := []string{"build"}
	if config.File != "" {
		bargs = append(bargs, "-f", config.File)
	}

	for _, tag := range config.Tags {
		if tag == GitTag {
			gtag, err := outCommand(tmpdir, "git", "describe", "--tags", "--abbrev=0")
			if err != nil {
				return fmt.Errorf("error getting git tag: %w", err)
			}
			bargs = append(bargs, "-t", config.Image+":"+gtag)
		} else if tag == GitSha {
			gsha, err := outCommand(tmpdir, "git", "rev-parse", "--short", "HEAD")
			if err != nil {
				return fmt.Errorf("error getting git sha: %w", err)
			}
			bargs = append(bargs, "-t", config.Image+":"+gsha)
		} else {
			bargs = append(bargs, config.Image+":"+tag)
		}
	}

	if config.Platform != "" {
		bargs = append(bargs, "--platform", config.Platform)
	}

	if config.Context == "" {
		bargs = append(bargs, ".")
	} else {
		bargs = append(bargs, config.Context)
	}
	if err := runCommand(tmpdir, "docker", bargs...); err != nil {
		return fmt.Errorf("error building image: %w", err)
	}

	if err := runCommand(tmpdir, "docker", "push", config.Image); err != nil {
		return fmt.Errorf("error pushing image: %w", err)
	}
	return nil
}

func deploy(tmp string, config Deploy) error {
	var dargs []string
	if config.Context != "" {
		dargs = append(dargs, "--context", config.Context)
	}
	dargs = append(dargs, "stack", "deploy", "-d=false")
	if config.File != "" {
		dargs = append(dargs, "-c", config.File)
	}
	if config.Auth != "" {
		dargs = append(dargs, "--with-registry-auth")
	}
	dargs = append(dargs, config.Name)

	for k, v := range config.Env {
		os.Setenv(k, v)
	}
	if err := runCommand(tmp, "docker", dargs...); err != nil {
		return fmt.Errorf("error deploying stack: %w", err)
	}
	return nil
}

func runCommand(dir string, name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	log.Println(strings.Join(cmd.Args, " "))
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func outCommand(dir string, name string, arg ...string) (string, error) {
	cmd := exec.Command(name, arg...)
	cmd.Stderr = os.Stderr
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
