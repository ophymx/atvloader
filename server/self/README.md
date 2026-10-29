Drop the built TV app here as `atvloader.apk` before building the server
(`make apk` does this). It is embedded into the binary and imported into the
catalog on startup as a protected app, which is how TVs update ATV Loader itself.
