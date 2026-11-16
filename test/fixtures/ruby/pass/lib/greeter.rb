# frozen_string_literal: true

require_relative 'greeter/clock'

# Greets people in two tones.
module Greeter
  TONES = %i[plain warm].freeze

  module_function

  def greet(name, tone = :plain)
    trimmed = name.strip
    raise ArgumentError, 'name must not be empty' if trimmed.empty?

    case tone
    when :plain then "Hello, #{trimmed}."
    when :warm then "Hello, #{trimmed}! Good to see you."
    else raise ArgumentError, "unknown tone #{tone}"
    end
  end
end
