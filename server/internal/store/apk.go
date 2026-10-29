package store

import (
	"image"
	_ "image/jpeg"
	_ "image/png"

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
	return meta, nil
}
