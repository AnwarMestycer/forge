//! What the proxy does to traffic, and the dice that decide it per chunk.

use std::time::Duration;

use rand::rngs::StdRng;
use rand::{RngExt, SeedableRng};

/// TCP gives up on a segment after this many retransmissions. Linux's own
/// limit (`net.ipv4.tcp_retries2`) is 15, ~15 minutes; 6 keeps a fully lossy
/// link failing in seconds rather than minutes.
pub const MAX_RETRANSMITS: u32 = 6;
/// Linux caps the retransmission timeout at 120 s (`TCP_RTO_MAX`).
pub const MAX_RTO: Duration = Duration::from_secs(120);

/// The faults applied to every connection, in both directions.
#[derive(Clone, Debug)]
pub struct Faults {
    /// One-way delay added to every chunk.
    pub latency: Duration,
    /// Each chunk's delay is `latency` ± a uniform random amount up to this.
    pub jitter: Duration,
    /// Probability (0–1) that a chunk is lost and has to be retransmitted.
    ///
    /// The proxy sees a byte stream, not packets, and discarding bytes would
    /// corrupt it — something real loss never does, because TCP retransmits.
    /// What an app actually feels from loss is the wait: the sender notices
    /// after a retransmission timeout (RTO) and sends again, and every byte
    /// behind it waits too (head-of-line blocking). So a lost chunk costs one
    /// `rto`, doubling for each further loss of the same chunk, and after
    /// `MAX_RETRANSMITS` the connection is reset, as TCP gives up.
    pub loss: f64,
    /// First retransmission timeout. Linux's minimum is 200 ms.
    pub rto: Duration,
    /// Probability (0–1) per chunk that the connection is reset (RST) instead.
    pub reset: f64,
    /// Cap on throughput, in bytes per second, for each direction.
    pub bandwidth: Option<u64>,
}

impl Default for Faults {
    fn default() -> Self {
        Self {
            latency: Duration::ZERO,
            jitter: Duration::ZERO,
            loss: 0.0,
            rto: Duration::from_millis(200),
            reset: 0.0,
            bandwidth: None,
        }
    }
}

/// Parses a rate in bytes per second: `500`, `64K` (KiB/s), `1.5M` (MiB/s).
pub fn parse_bandwidth(s: &str) -> Result<u64, String> {
    let (num, unit) = match s.trim().char_indices().last() {
        Some((i, c)) if c.is_ascii_alphabetic() => (&s[..i], c.to_ascii_uppercase()),
        _ => (s, ' '),
    };
    let mult = match unit {
        ' ' => 1.0,
        'K' => 1024.0,
        'M' => 1024.0 * 1024.0,
        'G' => 1024.0 * 1024.0 * 1024.0,
        _ => return Err(format!("{s:?}: unit must be K, M or G (bytes per second)")),
    };
    match num.trim().parse::<f64>() {
        Ok(n) if n * mult >= 1.0 => Ok((n * mult) as u64),
        _ => Err(format!(
            "{s:?} is not a rate like 500, 64K or 1.5M (bytes per second)"
        )),
    }
}

/// What happens to one chunk.
#[derive(Debug, PartialEq)]
pub enum Fate {
    /// Forward it this long after it was read; `retransmits` losses included.
    Deliver { delay: Duration, retransmits: u32 },
    /// Reset the connection after waiting this long (zero for an injected
    /// reset; the retransmission time spent for a link that gave up).
    Reset { after: Duration },
}

/// Per-direction fault decisions. Seeded, so a run can be replayed exactly.
pub struct Dice {
    faults: Faults,
    rng: StdRng,
}

impl Dice {
    pub fn new(faults: Faults, seed: u64) -> Self {
        Self {
            faults,
            rng: StdRng::seed_from_u64(seed),
        }
    }

    /// Decides one chunk's fate.
    pub fn roll(&mut self) -> Fate {
        if self.faults.reset > 0.0 && self.rng.random_bool(self.faults.reset) {
            return Fate::Reset {
                after: Duration::ZERO,
            };
        }
        let mut delay = self.delay();
        let mut rto = self.faults.rto;
        let mut retransmits = 0;
        while self.faults.loss > 0.0 && self.rng.random_bool(self.faults.loss) {
            if retransmits == MAX_RETRANSMITS {
                return Fate::Reset { after: delay };
            }
            delay += rto;
            rto = (rto * 2).min(MAX_RTO); // exponential backoff
            retransmits += 1;
        }
        Fate::Deliver { delay, retransmits }
    }

    /// Latency ± jitter, never negative.
    fn delay(&mut self) -> Duration {
        let Faults {
            latency, jitter, ..
        } = self.faults;
        if jitter.is_zero() {
            return latency;
        }
        let j = jitter.as_secs_f64();
        let d = latency.as_secs_f64() + self.rng.random_range(-j..=j);
        Duration::from_secs_f64(d.max(0.0))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ms(n: u64) -> Duration {
        Duration::from_millis(n)
    }

    #[test]
    fn no_jitter_is_exactly_the_latency() {
        let mut d = Dice::new(
            Faults {
                latency: ms(50),
                ..Default::default()
            },
            1,
        );
        assert!((0..100).all(|_| d.delay() == ms(50)));
    }

    #[test]
    fn jitter_stays_in_range_and_never_goes_negative() {
        let mut d = Dice::new(
            Faults {
                latency: ms(10),
                jitter: ms(30),
                ..Default::default()
            },
            7,
        );
        let delays: Vec<_> = (0..1000).map(|_| d.delay()).collect();
        assert!(delays.iter().all(|&x| x <= ms(40)));
        assert!(
            delays.contains(&Duration::ZERO),
            "latency - jitter < 0 clamps to zero"
        );
        assert!(
            delays.iter().any(|&x| x > ms(30)),
            "the upper part of the range is reached"
        );
    }

    #[test]
    fn same_seed_same_delays() {
        let f = Faults {
            latency: ms(100),
            jitter: ms(50),
            loss: 0.3,
            reset: 0.05,
            ..Default::default()
        };
        let a: Vec<_> = {
            let mut d = Dice::new(f.clone(), 42);
            (0..50).map(|_| d.roll()).collect()
        };
        let b: Vec<_> = {
            let mut d = Dice::new(f, 42);
            (0..50).map(|_| d.roll()).collect()
        };
        assert_eq!(a, b);
    }

    #[test]
    fn a_lost_chunk_waits_one_rto_then_backs_off() {
        let mut d = Dice::new(
            Faults {
                latency: ms(10),
                loss: 0.5,
                rto: ms(100),
                ..Default::default()
            },
            3,
        );
        for _ in 0..500 {
            match d.roll() {
                // 10 + 100 + 200 + 400 + ... : the delay pins down the count.
                Fate::Deliver { delay, retransmits } => {
                    let backoff = ms(100) * ((1 << retransmits) - 1);
                    assert_eq!(delay, ms(10) + backoff);
                }
                Fate::Reset { after } => assert_eq!(after, ms(10) + ms(100) * 63),
            }
        }
    }

    #[test]
    fn total_loss_gives_up_with_a_reset() {
        let mut d = Dice::new(
            Faults {
                loss: 1.0,
                rto: ms(200),
                ..Default::default()
            },
            1,
        );
        // 200 + 400 + 800 + 1600 + 3200 + 6400 ms of retransmissions.
        assert_eq!(d.roll(), Fate::Reset { after: ms(12_600) });
    }

    #[test]
    fn certain_reset_is_immediate() {
        let mut d = Dice::new(
            Faults {
                latency: ms(500),
                reset: 1.0,
                ..Default::default()
            },
            1,
        );
        assert_eq!(
            d.roll(),
            Fate::Reset {
                after: Duration::ZERO
            }
        );
    }

    #[test]
    fn no_faults_delivers_everything_untouched() {
        let mut d = Dice::new(Faults::default(), 9);
        assert!((0..1000).all(|_| d.roll()
            == Fate::Deliver {
                delay: Duration::ZERO,
                retransmits: 0
            }));
    }

    #[test]
    fn parses_bandwidth_with_binary_suffixes() {
        assert_eq!(parse_bandwidth("500"), Ok(500));
        assert_eq!(parse_bandwidth("64K"), Ok(64 * 1024));
        assert_eq!(parse_bandwidth("1.5m"), Ok(1_572_864));
        assert!(parse_bandwidth("0").is_err());
        assert!(parse_bandwidth("fast").is_err());
    }
}
