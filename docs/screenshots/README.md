# Screenshots

The admin and landing page images are headless Chromium captures of a demo
store (1500×940 and 1280×800) populated with a handful of public APKs.

TV images are taken on a real device with `adb exec-out screencap -p > tv-shelf.png`
(the app is Compose for TV, there is no emulator in CI). Wanted:

- `tv-shelf.png`: the main screen with a few rows and the self-update banner if one is pending
- `tv-app.png`: an app page with Install/Update, the version list and a changelog note
- `tv-pairing.png`: the six-character code screen
