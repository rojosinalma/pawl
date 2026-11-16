# frozen_string_literal: true

require 'test_helper'

class GreeterTest < Minitest::Test
  def test_greets_plainly_by_default
    assert_equal 'Hello, Ada.', Greeter.greet(' Ada ')
  end

  def test_greets_warmly
    assert_equal 'Hello, Ada! Good to see you.', Greeter.greet('Ada', :warm)
  end

  def test_rejects_an_empty_name
    assert_raises(ArgumentError) { Greeter.greet('  ') }
  end

  def test_day_part_reads_the_clock
    assert_equal :morning, Greeter::DayPart.new(-> { Time.new(2026, 1, 1, 9) }).call
    assert_equal :day, Greeter::DayPart.new(-> { Time.new(2026, 1, 1, 15) }).call
  end
end
