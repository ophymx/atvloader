package store

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"

	"github.com/shogo82148/androidbinary"
	"github.com/shogo82148/androidbinary/apk"
)

type apkMeta struct {
	Package     string
	Label       string
	VersionCode int64
	VersionName string
	MinSDK      int32
	TargetSDK   int32
	Icon        image.Image
}

// parseAPK pulls the manifest fields and the best raster icon out of an APK.
func parseAPK(path string) (*apkMeta, error) {
	a, err := apk.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer a.Close()

	m := a.Manifest()
	meta := &apkMeta{Package: a.PackageName()}
	if meta.Package == "" {
		if meta.Package, err = m.Package.String(); err != nil {
			return nil, err
		}
	}
	if vc, err := m.VersionCode.Int32(); err == nil {
		meta.VersionCode = int64(vc)
	}
	meta.VersionName, _ = m.VersionName.String()
	meta.MinSDK, _ = m.SDK.Min.Int32()
	meta.TargetSDK, _ = m.SDK.Target.Int32()
	meta.Label, _ = a.Label(nil)

	// Adaptive icons (API 26+) are XML and cannot be decoded, so ask for
	// pre-26 raster variants at a few densities, then fall back to default.
	configs := []*androidbinary.ResTableConfig{
		{SDKVersion: 25, Density: 640},
		{SDKVersion: 25, Density: 480},
		{SDKVersion: 25, Density: 320},
		{SDKVersion: 25},
		nil,
	}
	for _, c := range configs {
		if img, err := a.Icon(c); err == nil && img != nil {
			meta.Icon = img
			break
		}
	}
	if meta.Icon == nil {
		// No raster at all: the icon is an <adaptive-icon> or <vector> XML
		// (API 26+ apps often ship nothing else). Render it ourselves.
		cfg := &androidbinary.ResTableConfig{SDKVersion: 34, Density: 640}
		if iconPath, err := m.App.Icon.WithResTableConfig(cfg).String(); err == nil && !androidbinary.IsResID(iconPath) {
			img, err := renderIconFile(path, iconPath, iconSize)
			if err != nil {
				slog.Debug("render xml icon", "apk", path, "icon", iconPath, "err", err)
			}
			meta.Icon = img
		}
	}
	return meta, nil
}

// iconSize is the edge of icons rendered from XML drawables.
const iconSize = 256
