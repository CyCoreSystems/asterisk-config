package main

import (
	"archive/zip"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/CyCoreSystems/asterisk-config/template"
	"github.com/CyCoreSystems/gami"
	"github.com/CyCoreSystems/netdiscover/discover"
	"github.com/pkg/errors"
)

func main() {

	renderChan := make(chan error, 1)

	cloud := "gcp"
	if os.Getenv("CLOUD") != "" {
		cloud = os.Getenv("CLOUD")
	}
	disc := getDiscoverer(cloud)

	e := template.NewEngine(renderChan, disc, genSecret())

	source := "/source/asterisk-config.zip"
	if os.Getenv("SOURCE") != "" {
		source = os.Getenv("SOURCE")
	}

	defaultsRoot := "/defaults"
	if os.Getenv("DEFAULTS_DIR") != "" {
		defaultsRoot = os.Getenv("DEFAULTS_DIR")
	}

	customRoot := "/custom"
	if os.Getenv("CUSTOM_DIR") != "" {
		customRoot = os.Getenv("CUSTOM_DIR")
	}

	exportRoot := "/etc/asterisk"
	if os.Getenv("EXPORT_DIR") != "" {
		exportRoot = os.Getenv("EXPORT_DIR")
	}

	modules := "pjsip"
	if os.Getenv("RELOAD_MODULES") != "" {
		modules = os.Getenv("RELOAD_MODULES")
	}

	// Export defaults
	if err := render(e, defaultsRoot, exportRoot); err != nil {
		log.Println("failed to render defaults", err.Error())
		os.Exit(1)
	}

	// Extract the source
	if err := extractSource(source, customRoot); err != nil {
		log.Printf("failed to load source from %s: %s\n", source, err.Error())
	}

	// Execute the first render
	if err := render(e, customRoot, exportRoot); err != nil {
		log.Println("failed to render configuration:", err.Error())
		os.Exit(1)
	}

	for {
		if err := <-renderChan; err != nil {
			log.Println("failure during watch:", err.Error())
			break
		}

		if err := render(e, customRoot, exportRoot); err != nil {
			log.Println("failed to render:", err.Error())
			break
		}

		if err := reload(modules); err != nil {
			log.Println("failed to reload asterisk modules:", err.Error())
			break
		}
	}

	e.Close()
	os.Exit(1)
}

func getDiscoverer(cloud string) discover.Discoverer {
	switch cloud {
	case "aws":
		return discover.NewAWSDiscoverer()
	case "azure":
		return discover.NewAzureDiscoverer()
	case "digitalocean":
		return discover.NewDigitalOceanDiscoverer()
	case "gcp":
		return discover.NewGCPDiscoverer()
	default:
		return discover.NewDiscoverer()
	}
}

func render(e *template.Engine, customRoot string, exportRoot string) error {

	var fileCount int

	err := filepath.Walk(customRoot, func(fn string, info os.FileInfo, err error) error {
		if err != nil {
			return errors.Wrapf(err, "failed to access file %s", fn)
		}

		if info.IsDir() {
			return nil
		}
		fileCount++

		isTemplate := path.Ext(fn) == ".tmpl"

		outFile := path.Join(exportRoot, strings.TrimPrefix(fn, customRoot))
		if isTemplate {
			outFile = strings.TrimSuffix(outFile, ".tmpl")
		}

		out, err := os.Create(outFile)
		if err != nil {
			return errors.Wrapf(err, "failed to open file for writing: %s", outFile)
		}
		defer out.Close() // nolint: errcheck

		in, err := os.Open(fn)
		if err != nil {
			return errors.Wrapf(err, "failed to open template for reading: %s", fn)
		}
		defer in.Close() // nolint: errcheck

		if isTemplate {
			return template.Render(e, in, out)
		}

		_, err = io.Copy(out, in)
		return err
	})

	if err != nil {
		return err
	}

	if fileCount < 1 {
		return errors.New("no files processed")
	}

	return nil
}

func reload(modules string) error {

	list := strings.Split(modules, ",")

	ami, err := gami.Dial("127.0.0.1:5038")
	if err != nil {
		return err
	}

	ami.Run()
	defer ami.Close()

	for _, m := range list {
		_, err = ami.Action("reload", gami.Params{
			"ActionID": genSecret(),
			"Module":   m,
		})
		if err != nil {
			return errors.Wrapf(err, "failed to execute module reload for module (%s):", m)
		}
	}
	return nil
}

func extractSource(source, customRoot string) (err error) {

	if strings.HasPrefix(source, "http") {
		source, err = downloadSource(source)
		if err != nil {
			return errors.Wrap(err, "failed to download source")
		}
	}

	r, err := zip.OpenReader(source)
	if err != nil {
		return errors.Wrap(err, "failed to open source archive")
	}
	defer r.Close() // nolint: errcheck

	for _, f := range r.File {

		in, err := f.Open()
		if err != nil {
			return errors.Wrapf(err, "failed to read file %s", f.Name)
		}
		defer in.Close() // nolint: errcheck

		dest := path.Join(customRoot, f.Name)
		if f.FileInfo().IsDir() {
			if err = os.MkdirAll(dest, os.ModePerm); err != nil {
				return errors.Wrapf(err, "failed to create destination directory %s", f.Name)
			}
			continue
		}

		if err = os.MkdirAll(path.Dir(dest), os.ModePerm); err != nil {
			return errors.Wrapf(err, "failed to create destination directory %s", path.Dir(dest))
		}

		out, err := os.Create(dest)
		if err != nil {
			return errors.Wrapf(err, "failed to create file %s", dest)
		}

		_, err = io.Copy(out, in)
		out.Close() // nolint
		if err != nil {
			return errors.Wrapf(err, "error writing file %s", dest)
		}

	}

	return errors.New("not implemented")
}

func downloadSource(uri string) (string, error) {
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return "", errors.Wrapf(err, "failed to construct web request to %s", uri)
	}

	if os.Getenv("URL_USERNAME") != "" {
		req.SetBasicAuth(os.Getenv("URL_USERNAME"), os.Getenv("URL_PASSWORD"))
	}
	if os.Getenv("URL_AUTHORIZATION") != "" {
		req.Header.Add("Authorization", os.Getenv("URL_AUTHORIZATION"))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() // nolint: errcheck

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", errors.Errorf("request failed: %s", resp.Status)
	}
	if resp.ContentLength < 1 {
		return "", errors.New("empty response")
	}

	tf, err := ioutil.TempFile("", "config-download")
	if err != nil {
		return "", errors.Wrap(err, "failed to create temporary file for download")
	}
	defer tf.Close() // nolint: errcheck

	_, err = io.Copy(tf, resp.Body)

	return tf.Name(), err
}
