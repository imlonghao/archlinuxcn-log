package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/buildkite/terminal-to-html/v3"
	"github.com/getsentry/sentry-go"
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

func getMaintainers(path string) (map[string][]string, error) {
	r := make(map[string][]string)
	packages, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}
	for _, pkg := range packages {
		if strings.HasPrefix(pkg.Name(), ".") {
			continue
		}
		path := fmt.Sprintf("%s/%s/lilac.yaml", path, pkg.Name())
		conf, err := ioutil.ReadFile(path)
		if err != nil {
			return nil, err
		}
		pkgInfo := Lilac{}
		err = yaml.Unmarshal(conf, &pkgInfo)
		if err != nil {
			return nil, err
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
	return r, nil
}

func parseBuildLog(path string) (map[string]BuildLog, error) {
	r := make(map[string]BuildLog)
	buildLog, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	buildLogSplited := strings.Split(string(buildLog), "\n")
	for _, line := range buildLogSplited {
		regexResult := buildLogRegex.FindAllStringSubmatch(line, -1)
		if len(regexResult) == 0 {
			continue
		}
		during, err := strconv.Atoi(regexResult[0][5])
		if err != nil {
			return nil, err
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
	return r, nil
}

func log2html(src, dst, timestmap string) error {
	tsByte, err := ioutil.ReadFile(timestmap)
	if err != nil {
		return err
	}
	ts, err := strconv.Atoi(string(tsByte))
	if err != nil {
		return err
	}
	folders, err := ioutil.ReadDir(src)
	if err != nil {
		return err
	}
	for _, folder := range folders {
		t, err := time.Parse("2006-01-02T15:04:05", folder.Name())
		if err != nil {
			return err
		}
		if int(t.Unix()) < ts {
			continue
		}
		ts = int(t.Unix())
		err = ioutil.WriteFile(timestmap, []byte(strconv.Itoa(ts)), 0644)
		if err != nil {
			return err
		}
		packages, err := ioutil.ReadDir(fmt.Sprintf("%s/%s", src, folder.Name()))
		if err != nil {
			return err
		}
		for _, p := range packages {
			pSplit := strings.Split(p.Name(), ".")
			pName := strings.Join(pSplit[:len(pSplit)-1], ".")
			_, err := os.Stat(fmt.Sprintf("%s/%s", dst, pName))
			if err != nil {
				err := os.Mkdir(fmt.Sprintf("%s/%s", dst, pName), 0755)
				if err != nil {
					return err
				}
			}
			input, err := ioutil.ReadFile(fmt.Sprintf("%s/%s/%s", src, folder.Name(), p.Name()))
			if err != nil {
				return err
			}
			rendered := terminal.Render(input)
			rendered = bytes.Replace([]byte(PreviewTemplate), []byte("CONTENT"), rendered, 1)
			rendered = bytes.Replace(rendered, []byte("STYLESHEET"), MustAsset("assets/terminal.css"), 1)
			err = ioutil.WriteFile(fmt.Sprintf("%s/%s/%s.html", dst, pName, folder.Name()), rendered, 0644)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func joinLast(s []string) []string {
	if len(s) > 10 {
		return s[len(s)-10:]
	}
	return s
}

func main() {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:     os.Getenv("DSN"),
		Release: os.Getenv("COMMIT"),
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}
	defer sentry.Flush(time.Second * 2)

	maintainers, err := getMaintainers("/data/archgitrepo-webhook/archlinuxcn")
	if err != nil {
		sentry.CaptureException(err)
		return
	}
	buildlog, err := parseBuildLog("/home/lilydjwg/.lilac/build.log")
	if err != nil {
		sentry.CaptureException(err)
		return
	}
	err = log2html("/home/lilydjwg/.lilac/log", "/home/imlonghao/public_html/log", "/home/imlonghao/.config/log/timestamp")
	if err != nil {
		sentry.CaptureException(err)
		return
	}
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
		sentry.CaptureException(err)
		return
	}
	err = ioutil.WriteFile("/home/imlonghao/public_html/build-log.json", marshal, 0644)
	if err != nil {
		sentry.CaptureException(err)
		return
	}
}
