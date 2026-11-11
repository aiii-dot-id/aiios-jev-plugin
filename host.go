package main

import (
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"time"
)

type host interface {
	settings() (settingsReader, error)
	read(path string, offset int64, length int) (data []byte, eof bool, size int64, err error)
	list(folder string) (entries []sdk.FileEntry, truncated bool, err error)
	get(url string, o *sdk.HTTPOptions) (sdk.HTTPResult, error)
	post(url string, o *sdk.HTTPOptions) (sdk.HTTPResult, error)

	last() (time.Time, bool)
}

type sdkHost struct{}

func (sdkHost) settings() (settingsReader, error) { return sdk.Settings.Load() }
func (sdkHost) read(p string, off int64, n int) ([]byte, bool, int64, error) {
	return sdk.Files.Read(sdk.SandboxRoot, p, off, n)
}
func (sdkHost) list(folder string) ([]sdk.FileEntry, bool, error) {
	return sdk.Files.List(sdk.SandboxRoot, folder)
}
func (sdkHost) get(url string, o *sdk.HTTPOptions) (sdk.HTTPResult, error) {
	return sdk.HTTP.Get(url, o)
}
func (sdkHost) post(url string, o *sdk.HTTPOptions) (sdk.HTTPResult, error) {
	return sdk.HTTP.Post(url, o)
}
func (sdkHost) last() (time.Time, bool) { return sdk.LastHostTime() }
