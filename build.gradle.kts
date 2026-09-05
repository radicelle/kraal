plugins {
    kotlin("multiplatform") version "2.1.10"
}

group = "com.example"
version = "0.0.1"

repositories {
    mavenCentral()
}

val ktorVersion = "3.1.1"

kotlin {
    // Linux x64 executable target (used for Linux & Docker)
    linuxX64 {
        binaries {
            executable {
                entryPoint = "com.example.main"
                baseName = "kraal-server"
            }
        }
    }

    // Windows mingwX64 target (used for native Windows compilation if needed)
    mingwX64 {
        binaries {
            executable {
                entryPoint = "com.example.main"
                baseName = "kraal-server"
            }
        }
    }

    sourceSets {
        commonMain.dependencies {
            implementation("io.ktor:ktor-server-core:$ktorVersion")
            implementation("io.ktor:ktor-server-cio:$ktorVersion")
        }
        commonTest.dependencies {
            implementation(kotlin("test"))
            implementation("io.ktor:ktor-server-test-host:$ktorVersion")
        }
    }
}
