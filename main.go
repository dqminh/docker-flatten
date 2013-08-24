package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"text/template"
)

const DOCKER_PATH = "/var/lib/docker/graph"
const WHITEOUT = ".wh."
const Dockerfile = `
FROM {{.Base}}
MAINTAINER {{.Author}}
ADD {{.Archive}} /
{{if .HasPorts}} EXPOSE {{range $index, $port := .Ports}}{{$port}} {{end}} {{end}}
{{if .HasCmds}} CMD {{range $index, $cmd := .Cmds}}{{$cmd}} {{end}} {{end}}
`

type finalImage struct {
	Base               string
	NewName            string
	OriginalName       string
	Archive            string
	DockerfileCompiled *template.Template
	Images             []dockerImage
}

func (img finalImage) Author() string {
	return img.Images[0].Author
}

func (img finalImage) Ports() []string {
	return img.Images[0].Config.PortSpecs
}

func (img finalImage) HasPorts() bool {
	return len(img.Ports()) > 0
}

func (img finalImage) Cmds() []string {
	cmds := img.Images[0].Config.Cmd
	// ignore /bin/sh -c
	if len(cmds) > 2 {
		return cmds[2:len(cmds)]
	}
	return []string{}
}

func (img finalImage) HasCmds() bool {
	return len(img.Cmds()) > 0
}

type dockerHistory struct {
	Id        string
	Tags      []string
	CreatedBy string
}

type dockerImage struct {
	Id     string `json:"id"`
	Parent string `json:"parent"`
	Author string `json:"author"`
	Config struct {
		PortSpecs []string
		Cmd       []string
	} `json:"config"`
}

func (img dockerImage) Path() string {
	return path.Join("/var/lib/docker/graph", img.Id)
}

// returns a name that doesnt have invalid seperator
func normalizeStr(str string) string {
	return strings.Replace(str, "/", "-", -1)
}

func syncData(img finalImage) (string, error) {
	dir, err := ioutil.TempDir("/tmp", normalizeStr(img.OriginalName))
	if err != nil {
		return dir, err
	}
	// rsync old images first so data is overridden properly
	for i := len(img.Images) - 1; i >= 0; i-- {
		image := img.Images[i]
		err := exec.Command("sudo", "rsync", "-aHSx", "--devices", "--specials",
			path.Join(image.Path(), "layer"), dir).Run()
		if err != nil {
			return dir, err
		}
	}
	return dir, nil
}

func deleteWhiteouts(tmpImageDir string) error {
	return filepath.Walk(tmpImageDir, func(p string, info os.FileInfo, err error) error {
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		name := info.Name()
		parent := filepath.Dir(p)
		// if start with whiteout
		if strings.Index(name, WHITEOUT) == 0 {
			deletedFile := path.Join(parent, name[len(WHITEOUT):len(name)])
			// remove deleted files
			if err := os.RemoveAll(deletedFile); err != nil {
				return err
			}
			// remove the whiteout itself
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		}
		return nil
	})
}

func buildImage(archivePath string, img finalImage) error {
	archiveName := filepath.Base(archivePath)
	img.Archive = archiveName

	buildDir, err := ioutil.TempDir("/tmp", normalizeStr(img.NewName))
	if err != nil {
		return err
	}
	log("prepare a directory to build new image", buildDir)
	//defer os.RemoveAll(buildDir)

	log("copy archive file over")
	if err := exec.Command("cp", archivePath, buildDir).Run(); err != nil {
		return err
	}

	log("create dockerfile")
	dockerFile, err := os.Create(path.Join(buildDir, "Dockerfile"))
	if err != nil {
		return err
	}
	if err = img.DockerfileCompiled.Execute(dockerFile, img); err != nil {
		return err
	}

	if err := os.Chdir(buildDir); err != nil {
		return err
	}
	log("create new image")
	if err := exec.Command("docker", "build", "-t", img.NewName, ".").Run(); err != nil {
		return err
	}
	return nil
}

// given an image, return the current history of that image
func getHistory(image string) ([]dockerHistory, error) {
	history := []dockerHistory{}
	res, err := get("images/" + image + "/history")
	if err != nil {
		return history, err
	}
	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return history, err
	}
	err = json.Unmarshal(body, &history)
	if err != nil {
		return history, err
	}
	return history, nil
}

func getDockerImages(histories []dockerHistory) ([]dockerImage, error) {
	images := []dockerImage{}
	for _, history := range histories {
		img, err := inspectImage(history.Id)
		if err != nil {
			return images, err
		}
		images = append(images, img)
	}
	return images, nil
}

func inspectImage(id string) (img dockerImage, err error) {
	res, err := get("images/" + id + "/json")
	if err != nil {
		return
	}
	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return
	}
	err = json.Unmarshal(body, &img)
	return
}

func main() {
	DockerfileCompiled, err := template.New("dockerfile").Parse(Dockerfile)
	if err != nil {
		exit("failed to compile dockerfile template")
	}
	flTag := flag.String("t", "", "tag name of the new flattened image")
	flag.Parse()
	image := flag.Args()[0]
	if image == "" {
		exit("Missing image")
	}
	fmt.Printf("===> Flattenning image: %s into tag: %s\n", image, *flTag)

	log("get histories of the current image")
	histories, err := getHistory(image)
	if err != nil {
		exit("Failed to get history", image, err)
	}

	log("from the history, get list of images that formed the current image")
	images, err := getDockerImages(histories)
	if err != nil {
		exit("Failed to get info")
	}
	baseImageId := histories[len(histories)-1].Id
	final := finalImage{
		Base:               baseImageId,
		NewName:            *flTag,
		OriginalName:       image,
		DockerfileCompiled: DockerfileCompiled,
		Images:             images[0 : len(images)-1],
	}

	dataDir, err := syncData(final)
	if err != nil {
		exit("Failed to sync layers")
	}
	defer os.RemoveAll(dataDir)

	log("recursively delete whiteouts and deleted files in", dataDir)
	deleteWhiteouts(dataDir)

	log("prepare archived contents")
	archivePath := path.Join(dataDir, filepath.Base(dataDir)+".tar.gz")
	tar(path.Join(dataDir, "layer"), archivePath)

	buildImage(archivePath, final)

	log("===> Finished")
}

func get(path string) (*http.Response, error) {
	dial, err := net.Dial("unix", "/var/run/docker.sock")
	if err != nil {
		return nil, err
	}
	clientconn := httputil.NewClientConn(dial, nil)
	req, err := http.NewRequest("GET", fmt.Sprintf("/v1.4/%s", path), nil)
	if err != nil {
		return nil, err
	}
	res, err := clientconn.Do(req)
	if err != nil {
		// tries an older version of the API server with localhost:4243
		endpoint := "http://localhost:4243/v1.3/" + path
		return http.Get(endpoint)
	}
	return res, err
}

func exit(args ...interface{}) {
	fmt.Println(args...)
	os.Exit(1)
}

func log(args ...interface{}) {
	fmt.Println(args...)
}

func tar(dir string, archivePath string) error {
	return exec.Command("tar", "-C", dir, "--numeric-owner", "-czpf",
		archivePath, ".").Run()
}

func toString(b []byte, err error) (string, error) {
	return string(b), err
}
