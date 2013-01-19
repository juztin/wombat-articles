package handlers

import (
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"bitbucket.org/juztin/wombat"
	"bitbucket.org/juztin/wombat/imgconv"
)

func formFileImage(ctx wombat.Context, titlePath string) (string, *os.File, error) {
	// grab the image from the request
	f, h, err := ctx.Request.FormFile("image")
	if err != nil {
		return "", nil, err
	}
	defer f.Close()

	// create the project path
	imgPath := filepath.Join(imgRoot, titlePath)
	if err := os.MkdirAll(imgPath, 0755); err != nil {
		return "", nil, err
	}

	// replace spaces in image name
	imgName := strings.Replace(h.Filename, " ", "-", -1)

	// save the image, as a temp file
	//t, err := os.OpenFile(filepath.Join(imgPath, imgName), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	t, err := ioutil.TempFile(imgPath, "."+imgName)
	if err != nil {
		return imgName, nil, err
	}
	defer t.Close()
	// copy the image to the temp file
	if _, err := io.Copy(t, f); err != nil {
		return imgName, nil, err
	}

	return imgName, t, nil
}

func convertToJpg(imgName string, f *os.File, isThumb bool) (p string, i image.Image, err error) {
	x := filepath.Ext(imgName)
	//n = imgName[:len(imgName)-len(x)]+".jpg"
	s := ""
	n := imgName[:len(imgName)-len(x)]
	if isThumb {
		s = ".thumb"
	}
	p = fmt.Sprintf("%s%s.jpg", n, s)

	if isThumb {
		i, err = imgconv.ResizeWidthToJPG(f.Name(), p, true, 200)
	} else {
		i, err = imgconv.ConvertToJPG(f.Name(), p, true)
	}

	// delete the temporary image file on error
	if err != nil {
		os.Remove(f.Name())
		f = nil
	}

	return
}
