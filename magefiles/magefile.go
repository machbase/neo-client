package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/magefile/mage/mage"
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

var Default = Test

func Test() error {
	mg.Deps(CheckTmp)

	env := map[string]string{
		"GO111MODULE": "on",
		"CGO_ENABLED": "1",
	}

	if err := sh.RunWithV(env, "go", "mod", "tidy"); err != nil {
		return err
	}

	if err := sh.RunWithV(env, "go", "test", "-cover", "-coverprofile", "./tmp/cover.out",
		"./machrpc/...",
		"./driver/...",
	); err != nil {
		return err
	}
	if output, err := sh.Output("go", "tool", "cover", "-func=./tmp/cover.out"); err != nil {
		return err
	} else {
		lines := strings.Split(output, "\n")
		fmt.Println(lines[len(lines)-1])
	}
	fmt.Println("Test done.")
	return nil
}

func CheckTmp() error {
	_, err := os.Stat("tmp")
	if err != nil && err != os.ErrNotExist {
		err = os.Mkdir("tmp", 0755)
	} else if err != nil && err == os.ErrExist {
		return nil
	}
	return err
}

func CheckMoq() error {
	const moqRepo = "github.com/matryer/moq@latest"
	if _, err := sh.Output("moq", "-version"); err != nil {
		err = sh.RunV("go", "install", moqRepo)
		if err != nil {
			return err
		}
	}
	return nil
}

func Generate() error {
	mg.Deps(CheckMoq)
	return sh.RunV("go", "generate", "./...")
}

func Protoc() error {
	args := []string{}
	if len(args) == 0 {
		args = []string{
			"machrpc",
		}
	}

	for _, mod := range args {
		fmt.Printf("protoc regen %s/%s.proto...\n", mod, mod)
		sh.RunV("protoc", "-I", mod, mod+".proto",
			"--experimental_allow_proto3_optional",
			fmt.Sprintf("--go_out=./%s", mod), "--go_opt=paths=source_relative",
			fmt.Sprintf("--go-grpc_out=./%s", mod), "--go-grpc_opt=paths=source_relative",
		)
	}
	return nil
}

func Examples() error {
	return Example("")
}

func Example(name string) error {
	excludes := map[string]bool{
		"grpc_wave":       true,
		"http_wave":       true,
		"mqtt_subscriber": true,
	}
	examplesGoPath := `examples/go`
	if name == "" {
		entries, err := os.ReadDir(examplesGoPath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if _, ok := excludes[name]; ok {
				continue
			}
			path := filepath.Join(examplesGoPath, name, name+".go")
			if err := runGoExample(path); err != nil {
				fmt.Println("ERROR", path)
				fmt.Println(err.Error())
			}
		}
	} else {
		path := filepath.Join(examplesGoPath, name, name+".go")
		if err := runGoExample(path); err != nil {
			fmt.Println("ERROR", path)
			fmt.Println(err.Error())
		}
	}
	return nil
}

func runGoExample(path string) error {
	fmt.Println("--------")
	fmt.Println("EXAMPLE:", path)
	env := map[string]string{}
	if err := sh.RunWith(env, "go", "run", path); err != nil {
		return err
	}
	return nil
}
