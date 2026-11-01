plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

android {
    namespace = "dev.vpnctl.app"
    compileSdk = 35

    defaultConfig {
        applicationId = "dev.vpnctl.app"
        minSdk = 28
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"
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

dependencies {
    // the Go core, built by scripts/build-aar.sh: gomobile bind -target=android
    implementation(files("libs/vpnctl.aar"))

    implementation(platform("androidx.compose:compose-bom:2024.12.01"))
    implementation("androidx.compose.material3:material3")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.7")
    implementation("androidx.security:security-crypto:1.1.0-alpha06")

    // JVM unit tests: no emulator, no device. org.json ships as a stub in
    // android.jar, so the real implementation goes on the test classpath.
    testImplementation(kotlin("test"))
    testImplementation("org.json:json:20240303")
    testImplementation("junit:junit:4.13.2")
}
