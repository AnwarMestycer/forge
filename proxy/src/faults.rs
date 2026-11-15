//! What the proxy does to traffic, and the dice that decide it per chunk.

use std::time::Duration;

use rand::rngs::StdRng;
use rand::{RngExt, SeedableRng};

/// The faults applied to every connection, in both directions.
#[derive(Clone, Debug, Default)]
pub struct Faults {
    /// One-way delay added to every chunk.
    pub latency: Duration,
    /// Each chunk's delay is `latency` ± a uniform random amount up to this.
    pub jitter: Duration,
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

    /// How long after it was read a chunk may be written onward.
    pub fn delay(&mut self) -> Duration {
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
        };
        let a: Vec<_> = {
            let mut d = Dice::new(f.clone(), 42);
            (0..20).map(|_| d.delay()).collect()
        };
        let b: Vec<_> = {
            let mut d = Dice::new(f, 42);
            (0..20).map(|_| d.delay()).collect()
        };
        assert_eq!(a, b);
    }
}
