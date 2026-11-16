use acme_greeter::{Style, greet};

#[test]
fn loud_greeting_is_upper_case() {
    assert_eq!(greet(" Ada ", Style::Loud).unwrap(), "HELLO, ADA!");
}
