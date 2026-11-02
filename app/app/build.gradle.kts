plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

// A release build needs a keystore, and a keystore does not belong in the tree.
// The settings come from the environment; without them the release build is
// signed with the debug key, which is fine for a local check and nothing else.
val releaseKeystore = System.getenv("VPNCTL_KEYSTORE")
val appVersionCode = System.getenv("VPNCTL_VERSION_CODE")?.toIntOrNull() ?: 1
val appVersionName = System.getenv("VPNCTL_VERSION_NAME") ?: "0.1.0"

android {
    namespace = "dev.vpnctl.app"
    compileSdk = 35

    defaultConfig {
        applicationId = "dev.vpnctl.app"
        minSdk = 28
        targetSdk = 35
        versionCode = appVersionCode
        versionName = appVersionName
    }

    signingConfigs {
        if (releaseKeystore != null) {
            create("release") {
                storeFile = file(releaseKeystore)
                storePassword = System.getenv("VPNCTL_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("VPNCTL_KEY_ALIAS")
                keyPassword = System.getenv("VPNCTL_KEY_PASSWORD")
            }
        }
    }

    // The Go core ships as a native library per ABI, so a universal APK carries
    // four copies of it. The release splits the two phone ABIs; the debug build
    // stays universal because the emulator is x86_64.
    splits {
        abi {
            isEnable = System.getenv("VPNCTL_SPLIT_ABI") == "1"
            reset()
            include("arm64-v8a", "armeabi-v7a")
            isUniversalApk = false
        }
    }

    buildTypes {
        release {
            // the core is a gomobile AAR and the UI is Compose: R8 has nothing
            // to strip here, and a rule file nobody wrote is worse than none
            isMinifyEnabled = false
            signingConfig = signingConfigs.findByName("release") ?: signingConfigs.getByName("debug")
        }
    }

    buildFeatures {
        compose = true
    }

    testOptions {
        unitTests.isIncludeAndroidResources = false
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }
}

// The Go core reaches the app as a flatDir .aar, so Gradle has no way to know it
// is stale: a Go change that was not rebuilt silently ran old code, twice. The
// script is the only builder of that file, so the build runs it.
val buildAar by tasks.registering(Exec::class) {
    val root = rootProject.projectDir.parentFile

    workingDir = root
    commandLine("sh", "scripts/build-aar.sh")

    inputs.dir(root.resolve("internal"))
    inputs.dir(root.resolve("mobile"))
    inputs.dir(root.resolve("cmd"))
    inputs.file(root.resolve("go.mod"))
    inputs.file(root.resolve("go.sum"))
    outputs.file(root.resolve("app/app/libs/vpnctl.aar"))
}

tasks.named("preBuild") {
    dependsOn(buildAar)
}

dependencies {
    // the Go core, built by scripts/build-aar.sh: gomobile bind -target=android.
    // A flatDir dependency, not files(): a local .aar added as files() does not
    // put its classes on the compile classpath.
    implementation(mapOf("name" to "vpnctl", "ext" to "aar"))

    implementation(platform("androidx.compose:compose-bom:2024.12.01"))
    implementation("androidx.compose.material3:material3")
    // the bottom bar and the header actions are outlined Material Symbols; core
    // carries about forty of them and the designs use a different forty
    implementation("androidx.compose.material:material-icons-extended")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.7")
    implementation("androidx.security:security-crypto:1.1.0-alpha06")

    // QR for the subscription link: the card is the handoff, and a link someone
    // retypes from a phone screen is how a token leaks into a chat
    implementation("com.google.zxing:core:3.5.3")

    // JVM unit tests: no emulator, no device. org.json ships as a stub in
    // android.jar, so the real implementation goes on the test classpath.
    testImplementation(kotlin("test"))
    testImplementation("org.json:json:20240303")
    testImplementation("junit:junit:4.13.2")
}
