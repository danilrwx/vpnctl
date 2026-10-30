//! Генерация секретов. Вызывается один раз при создании сущности; рендер только читает.
use crate::state::{KeyPair, RealityKeys};
use base64::Engine as _;
use rand::RngCore;

fn random(n: usize) -> Vec<u8> {
    let mut b = vec![0u8; n];
    rand::rngs::OsRng.fill_bytes(&mut b);
    b
}

fn hex(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}

fn x25519() -> ([u8; 32], [u8; 32]) {
    let s = x25519_dalek::StaticSecret::random_from_rng(rand::rngs::OsRng);
    let p = x25519_dalek::PublicKey::from(&s);
    (s.to_bytes(), p.to_bytes())
}

/// Ключи Reality: base64url без паддинга, как у xray/sing-box.
pub fn reality() -> RealityKeys {
    let (s, p) = x25519();
    let e = base64::engine::general_purpose::URL_SAFE_NO_PAD;
    RealityKeys {
        private: e.encode(s),
        public: e.encode(p),
        short_ids: vec![short_id()],
    }
}

pub fn short_id() -> String {
    hex(&random(8))
}

/// Ключи WireGuard/AWG: обычный base64.
pub fn awg() -> KeyPair {
    let (s, p) = x25519();
    let e = base64::engine::general_purpose::STANDARD;
    KeyPair {
        private: e.encode(s),
        public: e.encode(p),
    }
}

pub fn preshared() -> String {
    base64::engine::general_purpose::STANDARD.encode(random(32))
}

pub fn password() -> String {
    base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(random(24))
}

pub fn uuid() -> String {
    uuid::Uuid::new_v4().to_string()
}

pub fn sub_id() -> String {
    base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(random(12))
}

/// Секрет mtg в режиме fake-TLS: `ee` + 16 байт + hex домена-прикрытия.
pub fn mtg(domain: &str) -> String {
    format!("ee{}{}", hex(&random(16)), hex(domain.as_bytes()))
}

#[cfg(test)]
mod tests {
    #[test]
    fn shapes() {
        let r = super::reality();
        assert_eq!(r.private.len(), 43);
        assert_eq!(r.short_ids[0].len(), 16);
        assert_eq!(super::awg().public.len(), 44);
        assert!(super::mtg("example.ru").starts_with("ee"));
        assert_ne!(super::uuid(), super::uuid());
    }
}
