# Android app

The app is a thin Kotlin/Compose layer over the Go core in `mobile/`. Screens
come from the mockups; until then this is the platform setup only.

## Toolchain

Nothing of this is installed on the dev laptop yet, so the steps below are the
starting point:

1. **JDK 17** (Temurin or the distro package).
2. **Android command line tools** → `sdkmanager "platform-tools" "platforms;android-35"`.
3. **NDK** → `sdkmanager "ndk;27.0.12077973"`, then export `ANDROID_NDK_HOME`.
4. **gomobile**:
   ```sh
   go install golang.org/x/mobile/cmd/gomobile@latest
   gomobile init
   ```

## Bindings

```sh
cd vpnctl-go
gomobile bind -target=android -androidapi 26 -o apps/android/app/libs/vpnctl.aar ./mobile
```

That produces `vpnctl.aar` with a Kotlin class generated from `mobile.App`,
`mobile.UserInfo` and `mobile.Link`.

## Project skeleton

```
apps/android/
├── settings.gradle.kts
├── build.gradle.kts
└── app/
    ├── build.gradle.kts
    └── src/main/
        ├── AndroidManifest.xml
        └── kotlin/dev/vpnctl/MainActivity.kt
```

`app/libs/vpnctl.aar` is git-ignored: it is rebuilt from Go on every change.

## What the UI gets

```kotlin
val app = Mobile.open(stateDir, keyPath)   // state dir on the device
app.users()                                 // name, modes, protocols, tg, enabled
app.links(user)                             // label + url, ready to copy or show as QR
app.share(user)                             // ready-to-forward text
app.addUser(name, modes, protocols, tg)     // writes the state and commits
app.sync()                                  // git push
```

No SSH and no apply on the phone: those stay on the laptop.
