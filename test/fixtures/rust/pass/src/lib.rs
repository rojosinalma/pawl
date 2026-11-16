//! Builds greetings for named people.

use std::fmt;

/// The tone of a greeting.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Style {
    /// An ordinary greeting.
    Plain,
    /// A shouted greeting.
    Loud,
}

/// Why a greeting could not be built.
#[derive(Debug, PartialEq, Eq)]
pub enum GreetError {
    /// The name was blank.
    EmptyName,
}

impl fmt::Display for GreetError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            GreetError::EmptyName => write!(f, "name is empty"),
        }
    }
}

impl std::error::Error for GreetError {}

/// Returns the greeting for `name` in the given style.
///
/// # Errors
///
/// Returns [`GreetError::EmptyName`] when the name is blank.
pub fn greet(name: &str, style: Style) -> Result<String, GreetError> {
    let name = name.trim();
    if name.is_empty() {
        return Err(GreetError::EmptyName);
    }
    Ok(match style {
        Style::Plain => format!("Hello, {name}."),
        Style::Loud => format!("HELLO, {}!", name.to_uppercase()),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn plain() {
        assert_eq!(greet("Ada", Style::Plain), Ok("Hello, Ada.".to_string()));
    }

    #[test]
    fn blank() {
        assert_eq!(greet("  ", Style::Loud), Err(GreetError::EmptyName));
    }
}
