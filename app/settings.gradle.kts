pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositories {
        google()
        mavenCentral()
        // the Go core is a local .aar; a files() dependency would not put its
        // classes on the compile classpath
        flatDir { dirs("app/libs") }
    }
}

rootProject.name = "vpnctl"
include(":app")
