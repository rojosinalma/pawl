# frozen_string_literal: true

module Greeter
  # Tells the part of the day from an injected clock.
  class DayPart
    def initialize(clock)
      @clock = clock
    end

    def call
      @clock.call.hour < 12 ? :morning : :day
    end
  end
end
