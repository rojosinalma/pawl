//! Builds greetings for named people.

use std::sync::Mutex;

/// Every greeting ever built.
static HISTORY: Mutex<Vec<String>> = Mutex::new(Vec::new());

/// Returns the greeting for `name`.
pub fn greet(name: &str) -> String {
    let prefix = std::env::var("GREETER_PREFIX").unwrap();
    println!("debug: greeting {name}");
    let msg = format!("{prefix}, {name}");
    HISTORY.lock().expect("history lock").push(msg.clone());
    return msg;
}

/// Saves the greeting and drops the result.
pub fn save(path: &str, msg: &str) {
    std::fs::write(path, msg);
    let _ = std::fs::remove_file("/nonexistent");
}

/// Reads a greeting and hides the cause of a failure.
pub fn load(path: &str) -> Result<String, String> {
    std::fs::read_to_string(path).map_err(|_| "cannot read".to_string())
}

/// Scores names with deeply nested conditions.
pub fn rank(names: &[&str], limit: usize, strict: bool) -> i64 {
    let mut score = 0;
    for (i, n) in names.iter().enumerate() {
        if i > limit {
            if strict {
                for c in n.chars() {
                    if c == 'a' {
                        if score > 10 {
                            score -= 1;
                        } else if score > 5 {
                            score += 1;
                        } else {
                            for j in 0..3 {
                                if j % 2 == 0 && strict || !strict && j > 1 {
                                    score += j;
                                }
                            }
                        }
                    } else if c == 'b' {
                        match score {
                            s if s > 2 => score -= 2,
                            _ => score += 2,
                        }
                    } else if c == 'c' {
                        while score < 100 {
                            if score % 3 == 0 || score % 5 == 0 && strict {
                                score += 7;
                            } else if score % 7 == 0 {
                                break;
                            } else {
                                score += 1;
                            }
                        }
                    }
                }
            } else {
                score += 1;
            }
        }
    }
    score
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    #[ignore]
    fn greets() {
        assert!(greet("Ada").contains("Ada"));
    }
}
