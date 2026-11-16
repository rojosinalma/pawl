//! Writes a greeting for each argument.

use std::io::{self, Write};
use std::process::ExitCode;

use acme_greeter::{Style, greet};

fn main() -> ExitCode {
    let mut out = io::stdout().lock();
    for name in std::env::args().skip(1) {
        match greet(&name, Style::Plain) {
            Ok(line) => {
                if writeln!(out, "{line}").is_err() {
                    return ExitCode::FAILURE;
                }
            }
            Err(err) => {
                let _ignored = writeln!(io::stderr(), "greeter: {err}");
                return ExitCode::FAILURE;
            }
        }
    }
    ExitCode::SUCCESS
}
