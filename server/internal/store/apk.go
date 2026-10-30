package store

import (
	"encoding/xml"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"strings"

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
	Signer      *Signer
}

// appLabel resolves android:label. androidbinary stops at one level of
// indirection, but apps built with resource shrinking or string aliasing
// often have @string/app_name point at another string, which it reports as
// "invalid type: uint32"; chase the chain ourselves in that case.
func appLabel(path string, a *apk.Apk) string {
	if l, err := a.Label(nil); err == nil && strings.TrimSpace(l) != "" && !androidbinary.IsResID(l) {
		return strings.TrimSpace(l)
	}
	attr, _ := a.Manifest().App.Label.MarshalXMLAttr(xml.Name{})
	raw := strings.TrimSpace(attr.Value)
	if raw == "" {
		return ""
	}
	if !androidbinary.IsResID(raw) {
		return raw
	}
	id, err := androidbinary.ParseResID(raw)
	if err != nil {
		return ""
	}
	res, err := openAPKRes(path)
	if err != nil {
		return ""
	}
	defer res.Close()
	v, err := res.resolve(uint32(id))
	if err != nil {
		slog.Debug("resolve label", "apk", path, "label", raw, "err", err)
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
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
	if meta.Signer, err = apkSigner(path); err != nil {
		slog.Debug("read apk signer", "apk", path, "err", err)
		meta.Signer = nil
	}
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
	meta.Label = appLabel(path, a)

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
