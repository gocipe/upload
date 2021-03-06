package upload

import (
	"fmt"
	"github.com/h2non/filetype"
)

// ImageUploader is an image uploader
type ImageUploader struct {
	Options  *Options
	Processor *ImageProcessor
}

// NewImageUploader returns ImageUploader
func NewImageUploader(common *Options, opts ...OptionImage) *ImageUploader {
	processor := NewImageProcessor(opts...)
	return &ImageUploader{Options: common, Processor: processor}
}

// Upload method to satisfy uploader interface
func (u *ImageUploader) Upload(name string, content []byte) (*UploadedFile, error) {
	if !isValidImage(content) {
		return nil, fmt.Errorf("Not a valid image")
	}

	uploadedFile := NewUploadedFile(name, *u.Options)

	if err := uploadedFile.Save(content, true); err != nil {
		return nil, err
	}

	fileType, err := filetype.MatchFile(uploadedFile.DiskPath())
	if err != nil {
		return nil, fmt.Errorf("Error retrieving file type: %v", err)
	}

	newType := u.Options.ConvertTo(fileType)
	if err := uploadedFile.ChangeExt(newType.Extension); err != nil {
		return nil, err
	}

	return uploadedFile, nil
}
