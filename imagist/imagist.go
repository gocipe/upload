package imagist

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log"
	"os"

	"github.com/disintegration/imaging"
	"github.com/lsldigital/gocipe-upload/util"
	"github.com/pkg/errors"
	filetype "gopkg.in/h2non/filetype.v1"
)

const (
	// TypeImageJPG denotes image of file type jpg
	TypeImageJPG = "jpg"
	// TypeImageJPEG denotes image of file type jpeg
	TypeImageJPEG = "jpeg"
	// TypeImagePNG denotes image of file type png
	TypeImagePNG = "png"
)

// Anchor points for X,Y
const (
	Left = iota
	Right
	Top
	Bottom
	Center
)

var (
	// DefaultDimensions represent default dimensions to use, they have no limit (preserve original)
	DefaultDimensions = ImageDimensions{
		MinWidth:  util.NoLimit,
		MinHeight: util.NoLimit,
	}

	// Disk paths to static assets
	_diskPathWatermark string
	_diskPathBackdrop  string

	// _assetBox satisfies the AssetBoxer interface
	_assetBox assetBoxer

	// TopLeft is the top-left position for watermark
	TopLeft = &WatermarkPosition{Horizontal: Left, Vertical: Top}
	// TopCenter is the top-center position for watermark
	TopCenter = &WatermarkPosition{Horizontal: Center, Vertical: Top}
	// TopRight is the top-right position for watermark
	TopRight = &WatermarkPosition{Horizontal: Right, Vertical: Top}
	// CenterRight is the center-right position for watermark
	CenterRight = &WatermarkPosition{Horizontal: Right, Vertical: Center}
	// BottomRight is the bottom-right position for watermark
	BottomRight = &WatermarkPosition{Horizontal: Right, Vertical: Bottom}
	// BottomCenter is the bottom-center position for watermark
	BottomCenter = &WatermarkPosition{Horizontal: Center, Vertical: Bottom}
	// BottomLeft is the bottom-left position for watermark
	BottomLeft = &WatermarkPosition{Horizontal: Left, Vertical: Bottom}
	// CenterLeft is the center-left position for watermark
	CenterLeft = &WatermarkPosition{Horizontal: Left, Vertical: Center}

	_env = util.EnvironmentDEV
)

// Imagist is an image processing mechanism
type Imagist struct {
	jobs chan Job
	done chan string
}

// Job represents an image processing task
type Job struct {
	FileDiskPath string
	Config       *image.Config
	Dimensions   *ImageDimensions
}

// ImageDimensions holds dimensions options
type ImageDimensions struct {
	MinWidth  int
	MinHeight int
	Formats   []FormatDimensions
}

// FormatDimensions holds dimensions options for format
type FormatDimensions struct {
	Name      string
	Width     int
	Height    int
	Backdrop  bool               // (default: false) If true, will add a backdrop
	Watermark *WatermarkPosition // (default: nil) If not nil, will overlay an image as watermark at X,Y pos +-OffsetX,OffsetY
}

// WatermarkPosition holds the watermark position
type WatermarkPosition struct {
	Horizontal int
	Vertical   int
	OffsetX    int
	OffsetY    int
}

type subImager interface {
	SubImage(r image.Rectangle) image.Image
}

type assetBoxer interface {
	Open(string) (*os.File, error)
}

func init() {
	image.RegisterFormat("jpeg", "jpeg", jpeg.Decode, jpeg.DecodeConfig)
	image.RegisterFormat("png", "png", png.Decode, png.DecodeConfig)
	image.RegisterFormat("gif", "gif", gif.Decode, gif.DecodeConfig)
}

// SetEnv sets the environment gocipe-upload operates in
func SetEnv(env string) {
	switch env {
	case util.EnvironmentDEV, util.EnvironmentPROD:
		// We are good :)
	default:
		// Invalid environment
		return
	}
	_env = env
}

// SetBackdropImage sets the disk path for backdrop images
func SetBackdropImage(path string) {
	_diskPathBackdrop = path
}

// SetWatermarkImage sets the disk path for watermark images
func SetWatermarkImage(path string) {
	_diskPathWatermark = path
}

// New returns an instance of imagist, with the internal go routine awaiting jobs over the channel
func New(chansize ...int) *Imagist {
	var s int

	if len(chansize) == 0 {
		s = 10
	} else {
		s = chansize[0]
	}

	i := Imagist{
		jobs: make(chan Job, s),
		done: make(chan string, s),
	}

	go i.listen()

	return &i
}

//listen starts listening for jobs on the internal channel
func (i Imagist) listen() {
	jobs := make(map[string]interface{})

	for {
		select {
		case done := <-i.done:
			delete(jobs, done)
		case job := <-i.jobs:
			if _, exists := jobs[job.FileDiskPath]; !exists {
				jobs[job.FileDiskPath] = nil
				go i.execute(job)
			}
		}
	}
}

// Add creates a job entry for processing
func (i Imagist) Add(buf []byte, fileDiskPath string, dimensions *ImageDimensions, validate bool) error {
	if !filetype.IsImage(buf) {
		return fmt.Errorf("image type invalid")
	}

	if dimensions == nil {
		dimensions = &DefaultDimensions
	}

	config, imgType, err := image.DecodeConfig(bytes.NewReader(buf))
	if err != nil {
		log.Printf("error decoding image: %v", err)
		return err
	}

	switch imgType {
	case TypeImageJPG, TypeImageJPEG, TypeImagePNG:
		//all ok
	default:
		return fmt.Errorf("image type %s invalid", imgType)
	}

	if validate {
		// Check min width and height
		if dimensions.MinWidth != util.NoLimit && config.Width < dimensions.MinWidth {
			log.Printf("image %v lower than min width: %v\n", fileDiskPath, dimensions.MinWidth)
			return fmt.Errorf("image width less than %dpx", dimensions.MinWidth)
		}

		if dimensions.MinHeight != util.NoLimit && config.Height < dimensions.MinHeight {
			log.Printf("image %v lower than min height: %v\n", fileDiskPath, dimensions.MinHeight)
			return fmt.Errorf("image height less than %dpx", dimensions.MinHeight)
		}
	}

	job := Job{
		FileDiskPath: fileDiskPath,
		Config:       &config,
		Dimensions:   dimensions,
	}
	i.jobs <- job

	return nil
}

func (i Imagist) execute(j Job) {
	for _, format := range j.Dimensions.Formats {
		if format.Name == "" || format.Width <= 0 || format.Height <= 0 {
			continue
		}

		newWidth := format.Width
		newHeight := format.Height

		// Do not upscale
		if j.Config.Width < format.Width {
			newWidth = j.Config.Width
		}
		if j.Config.Height < j.Config.Height {
			newHeight = j.Config.Height
		}

		landscape := j.Config.Height < j.Config.Width

		imageProcess(j.FileDiskPath, newWidth, newHeight, landscape, format)
	}

	i.done <- j.FileDiskPath
}

func imageProcess(imgDiskPath string, newWidth, newHeight int, landscape bool, format FormatDimensions) error {
	var (
		img image.Image
		err error
	)

	img, err = imaging.Open(imgDiskPath)
	if err != nil {
		return errors.Wrap(err, "image open error")
	}

	// Do not crop and resize when using backdrop but downscale
	if format.Backdrop && !landscape {
		// Scale down srcImage to fit the bounding box
		img = imaging.Fit(img, newWidth, newHeight, imaging.Lanczos)

		// Open a new image to use as backdrop layer
		var back image.Image
		if _env == util.EnvironmentDEV {
			back, err = imaging.Open("../assets/" + _diskPathBackdrop)
		} else {
			var staticAsset *os.File
			staticAsset, err = _assetBox.Open(_diskPathBackdrop)
			if err != nil {
				// if err, fall back to a blue background backdrop
				back = imaging.New(format.Width, format.Height, color.NRGBA{0, 29, 56, 0})
			}
			defer staticAsset.Close()
			back, _, err = image.Decode(staticAsset)
		}

		if err != nil {
			// if err, fall back to a blue background backdrop
			back = imaging.New(format.Width, format.Height, color.NRGBA{0, 29, 56, 0})
		} else {
			// Resize and crop backdrop accordingly
			back = imaging.Fill(back, format.Width, format.Height, imaging.Center, imaging.Lanczos)
		}

		// Overlay image in center on backdrop layer
		img = imaging.OverlayCenter(back, img, 1.0)
	} else {
		// Resize and crop the image to fill the [newWidth x newHeight] area
		img = imaging.Fill(img, newWidth, newHeight, imaging.Center, imaging.Lanczos)
	}

	if format.Watermark != nil {
		var watermark image.Image
		if _env == util.EnvironmentDEV {
			watermark, err = imaging.Open("../assets/" + _diskPathWatermark + ":" + format.Name)
		} else {
			var staticAsset *os.File
			staticAsset, err = _assetBox.Open(_diskPathWatermark + ":" + format.Name)
			if err != nil {
				return errors.Wrap(err, "watermark not found")
			}
			defer staticAsset.Close()
			watermark, _, err = image.Decode(staticAsset)
		}
		if err == nil {
			bgBounds := img.Bounds()
			bgW := bgBounds.Dx()
			bgH := bgBounds.Dy()

			watermarkBounds := watermark.Bounds()
			watermarkW := watermarkBounds.Dx()
			watermarkH := watermarkBounds.Dy()

			var watermarkPos image.Point

			switch format.Watermark.Horizontal {
			default:
				format.Watermark.Horizontal = Left
				fallthrough
			case Left:
				watermarkPos.X += format.Watermark.OffsetX
			case Right:
				RightX := bgBounds.Min.X + bgW - watermarkW
				watermarkPos.X = RightX - format.Watermark.OffsetX
			case Center:
				CenterX := bgBounds.Min.X + bgW/2
				watermarkPos.X = CenterX - watermarkW/2 + format.Watermark.OffsetX
			}

			switch format.Watermark.Vertical {
			default:
				format.Watermark.Vertical = Top
				fallthrough
			case Top:
				watermarkPos.Y += format.Watermark.OffsetY
			case Bottom:
				BottomY := bgBounds.Min.Y + bgH - watermarkH
				watermarkPos.Y = BottomY - format.Watermark.OffsetY
			case Center:
				CenterY := bgBounds.Min.Y + bgH/2
				watermarkPos.Y = CenterY - watermarkH/2 + format.Watermark.OffsetY
			}

			img = imaging.Overlay(img, watermark, watermarkPos, 1.0)
		}
	}

	imagingFormat, err := imaging.FormatFromFilename(imgDiskPath)
	if err != nil {
		return errors.Wrap(err, "image get format error")
	}

	newDiskPath := imgDiskPath + ":" + format.Name

	outputFile, err := os.Create(newDiskPath)
	if err != nil {
		return errors.Wrap(err, "image get format error")
	}
	defer outputFile.Close()

	return imaging.Encode(outputFile, img, imagingFormat)
}
