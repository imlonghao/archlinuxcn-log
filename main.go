package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/buildkite/terminal-to-html"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Lilac struct {
	Maintainers []struct {
		Github string `yaml:"github"`
		Email  string `yaml:"email,omitempty"`
	} `yaml:"maintainers"`
}

type BuildLog struct {
	Time    string   `json:"time"`
	During  int      `json:"during"`
	Version string   `json:"version"`
	Result  []string `json:"result"`
}

type Render []struct {
	Name        string `json:"name"`
	Maintainers string `json:"maintainers"`
	BuildLog
}

var (
	buildLogRegex   = regexp.MustCompile(`\[(.*?)] (.*?) .*? \[(.*?)] (successful|failed) after (\d+)s`)
	PreviewTemplate = `
	<!DOCTYPE html>
	<html>
		<head>
			<meta charset="UTF-8">
			<title>terminal-to-html Preview</title>
			<style>STYLESHEET</style>
		</head>
		<body>
			<div class="term-container">CONTENT</div>
		</body>
	</html>
`
)

func getMaintainers(path string) map[string][]string {
	r := make(map[string][]string)
	packages, err := ioutil.ReadDir(path)
	if err != nil {
		log.Fatal(err)
	}
	for _, pkg := range packages {
		if strings.HasPrefix(pkg.Name(), ".") {
			continue
		}
		path := fmt.Sprintf("%s/%s/lilac.yaml", path, pkg.Name())
		conf, err := ioutil.ReadFile(path)
		if err != nil {
			log.Printf("fail to read %s, %v", path, err)
			continue
		}
		pkgInfo := Lilac{}
		err = yaml.Unmarshal(conf, &pkgInfo)
		if err != nil {
			log.Printf("fail to unmarshal %s, %v", path, err)
			continue
		}
		for _, maintainer := range pkgInfo.Maintainers {
			if value, ok := r[pkg.Name()]; ok {
				value = append(value, maintainer.Github)
				r[pkg.Name()] = value
			} else {
				r[pkg.Name()] = []string{maintainer.Github}
			}
		}
	}
	return r
}

func parseBuildLog(path string) map[string]BuildLog {
	r := make(map[string]BuildLog)
	buildLog, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	buildLogSplited := strings.Split(string(buildLog), "\n")
	for _, line := range buildLogSplited {
		regexResult := buildLogRegex.FindAllStringSubmatch(line, -1)
		if len(regexResult) == 0 {
			continue
		}
		during, err := strconv.Atoi(regexResult[0][5])
		if err != nil {
			log.Printf("fail to convert time to int %s, %v", regexResult[0][5], err)
			continue
		}
		result := "❌"
		if regexResult[0][4] == "successful" {
			result = "✅"
		}
		if v, ok := r[regexResult[0][2]]; ok {
			v.Time = regexResult[0][1]
			v.During = during
			v.Version = regexResult[0][3]
			v.Result = append(v.Result, result)
			r[regexResult[0][2]] = v
		} else {
			r[regexResult[0][2]] = BuildLog{
				Time:    regexResult[0][1],
				During:  during,
				Version: regexResult[0][3],
				Result:  []string{result},
			}
		}
	}
	return r
}

func log2html(src, dst, timestmap string) {
	tsByte, err := ioutil.ReadFile(timestmap)
	if err != nil {
		log.Fatal(err)
	}
	ts, err := strconv.Atoi(string(tsByte))
	if err != nil {
		log.Fatal(err)
	}
	folders, err := ioutil.ReadDir(src)
	if err != nil {
		log.Fatal(err)
	}
	for _, folder := range folders {
		t, err := time.Parse("2006-01-02T15:04:05", folder.Name())
		if err != nil {
			log.Fatal(err)
		}
		if int(t.Unix()) < ts {
			continue
		}
		ts = int(t.Unix())
		err = ioutil.WriteFile(timestmap, []byte(strconv.Itoa(ts)), 0644)
		if err != nil {
			log.Fatal(err)
		}
		packages, err := ioutil.ReadDir(fmt.Sprintf("%s/%s", src, folder.Name()))
		if err != nil {
			log.Fatal(err)
		}
		for _, p := range packages {
			pSplit := strings.Split(p.Name(), ".")
			pName := strings.Join(pSplit[:len(pSplit)-1], ".")
			_, err := os.Stat(fmt.Sprintf("%s/%s", dst, pName))
			if err != nil {
				err := os.Mkdir(fmt.Sprintf("%s/%s", dst, pName), 0755)
				if err != nil {
					log.Fatal(err)
				}
			}
			input, err := ioutil.ReadFile(fmt.Sprintf("%s/%s/%s", src, folder.Name(), p.Name()))
			if err != nil {
				log.Fatal(err)
			}
			rendered := terminal.Render(input)
			rendered = bytes.Replace([]byte(PreviewTemplate), []byte("CONTENT"), rendered, 1)
			rendered = bytes.Replace(rendered, []byte("STYLESHEET"), MustAsset("assets/terminal.css"), 1)
			err = ioutil.WriteFile(fmt.Sprintf("%s/%s/%s.html", dst, pName, folder.Name()), rendered, 0644)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}

func joinLast(s []string) []string {
	if len(s) > 10 {
		return s[len(s)-10:]
	}
	return s
}

func main() {
	maintainers := getMaintainers("/home/imlonghao/archlinuxcn/archlinuxcn")
	buildlog := parseBuildLog("/home/lilydjwg/.lilac/build.log")
	log2html("/home/lilydjwg/.lilac/log", "/home/imlonghao/public_html/log", "/home/imlonghao/.config/log/timestamp")
	table := Render{}
	for k, v := range buildlog {
		if len(maintainers[k]) == 0 {
			continue
		}
		v.Result = joinLast(v.Result)
		z := struct {
			Name        string `json:"name"`
			Maintainers string `json:"maintainers"`
			BuildLog
		}{
			Name:        k,
			Maintainers: strings.Join(maintainers[k], " / "),
			BuildLog:    v,
		}
		table = append(table, z)
	}
	marshal, err := json.Marshal(table)
	if err != nil {
		log.Fatal(err)
	}
	err = ioutil.WriteFile("/home/imlonghao/public_html/build-log.json", marshal, 0644)
	if err != nil {
		log.Fatal(err)
	}
}
