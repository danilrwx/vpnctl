//! Backend for the secrets file. The CLI shells out to `sops`; a mobile build
//! installs its own implementation before touching the state.
use anyhow::Result;
use std::path::Path;
use std::sync::OnceLock;

pub trait Secrets: Send + Sync {
    /// Decrypt `secrets.sops.yaml` at `path` and return the plain YAML.
    fn decrypt(&self, path: &Path) -> Result<String>;
    /// Encrypt `plain` for `path`, honouring the creation rules of its directory.
    fn encrypt(&self, path: &Path, plain: &str) -> Result<Vec<u8>>;
}

static IMPL: OnceLock<Box<dyn Secrets>> = OnceLock::new();

/// Installs the backend. The first call wins; later calls are ignored.
pub fn install(s: Box<dyn Secrets>) {
    let _ = IMPL.set(s);
}

pub fn current() -> &'static dyn Secrets {
    IMPL.get_or_init(default_backend).as_ref()
}

#[cfg(not(target_os = "android"))]
fn default_backend() -> Box<dyn Secrets> {
    Box::new(process::SopsProcess)
}

#[cfg(target_os = "android")]
fn default_backend() -> Box<dyn Secrets> {
    Box::new(missing::NoBackend)
}

#[cfg(target_os = "android")]
mod missing {
    use super::Secrets;
    use anyhow::Result;
    use std::path::Path;

    pub struct NoBackend;

    impl Secrets for NoBackend {
        fn decrypt(&self, _p: &Path) -> Result<String> {
            bail!("no secrets backend installed for this build")
        }
        fn encrypt(&self, _p: &Path, _plain: &str) -> Result<Vec<u8>> {
            bail!("no secrets backend installed for this build")
        }
    }
}

#[cfg(not(target_os = "android"))]
mod process {
    use super::Secrets;
    use anyhow::{Context, Result, bail};
    use std::io::Write;
    use std::path::Path;
    use std::process::{Command, Stdio};

    pub struct SopsProcess;

    impl Secrets for SopsProcess {
        fn decrypt(&self, path: &Path) -> Result<String> {
            let out = Command::new("sops")
                .arg("-d")
                .arg(path)
                .stderr(Stdio::inherit())
                .output()
                .context("run sops")?;
            if !out.status.success() {
                bail!("sops -d failed");
            }
            String::from_utf8(out.stdout).context("decode sops output")
        }

        fn encrypt(&self, path: &Path, plain: &str) -> Result<Vec<u8>> {
            let dir = path.parent().unwrap_or(Path::new("."));
            let mut child = Command::new("sops")
                .args([
                    "-e",
                    "--input-type",
                    "yaml",
                    "--output-type",
                    "yaml",
                    "--filename-override",
                ])
                .arg(path)
                .arg("/dev/stdin")
                .current_dir(dir)
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::inherit())
                .spawn()
                .context("run sops -e")?;
            child
                .stdin
                .take()
                .unwrap()
                .write_all(plain.as_bytes())
                .context("feed sops")?;
            let out = child.wait_with_output()?;
            if !out.status.success() {
                bail!("sops -e failed");
            }
            Ok(out.stdout)
        }
    }
}
