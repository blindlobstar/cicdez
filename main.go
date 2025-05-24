package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/getsops/sops/v3/decrypt"
	"gopkg.in/yaml.v3"
)

const (
	git_tag = "git_tag"
	git_sha = "git_sha"
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

	cmd := exec.Command("git", "clone", ".", tmp)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("error running git clone command: %s", err)
	}

	for _, pre := range t.Pre {
		args := strings.Split(pre, " ")
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			log.Fatalf("error running %s command: %s", pre, err)
		}
	}

	// ctx := context.Background()
	// cli, err := client.NewEnvClient()
	// if err != nil {
	// 	log.Fatalf("error initializing docker client: %s", err)
	// }
	// cli.ImageBuild(ctx, "", types.ImageBuildOptions{
	// 	Platform: t.Build.Platform,
	// 	Dockerfile: t.Build.File,
	// })
	bargs := []string{"build"}

	if t.Build.File != "" {
		bargs = append(bargs, "-f", t.Build.File)
	}

	cmd = exec.Command("git", "describe", "--tags", "--abbrev=0")
	cmd.Stderr = os.Stderr
	cmd.Dir = tmp
	tagout, err := cmd.Output()
	if err != nil {
		// log.Fatalf("error getting git tag: %s", err)
	}
	gtag := string(tagout)

	cmd = exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = tmp
	cmd.Stderr = os.Stderr
	shaout, err := cmd.Output()
	if err != nil {
		log.Fatalf("error getting git sha: %s", err)
	}
	gsha := string(shaout[:len(shaout)-1])

	for _, tag := range t.Build.Tags {
		if tag == git_tag {
			bargs = append(bargs, "-t", t.Build.Image+":"+gtag)
		} else if tag == git_sha {
			bargs = append(bargs, "-t", t.Build.Image+":"+gsha)
		} else {
			bargs = append(bargs, t.Build.Image+":"+tag)
		}
	}

	if t.Build.Platform != "" {
		bargs = append(bargs, "--platform", t.Build.Platform)
	}

	if t.Build.Context == "" {
		bargs = append(bargs, ".")
	} else {
		bargs = append(bargs, t.Build.Context)
	}
	cmd = exec.Command("docker", bargs...)
	cmd.Dir = tmp
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("error building image: %s", err)
	}

	cmd = exec.Command("docker", "push", t.Build.Image)
	cmd.Dir = tmp
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("error pushing image: %s", err)
	}

	var dargs []string
	if t.Deploy.Context != "" {
		dargs = append(dargs, "--context", t.Deploy.Context)
	}
	dargs = append(dargs, "stack", "deploy")
	if t.Deploy.File != "" {
		dargs = append(dargs, "-c", t.Deploy.File)
	}
	dargs = append(dargs, t.Deploy.Name)
	if t.Deploy.Auth != "" {
		dargs = append(dargs, "--with-registry-auth")
	}
	dargs = append(dargs, "-d", "false")

	for k, v := range t.Deploy.Env {
		os.Setenv(k, v)
	}
	cmd = exec.Command("docker", dargs...)
	log.Println(strings.Join(cmd.Args, " "))
	cmd.Dir = tmp
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("error deploying stack: %s", err)
	}
}
