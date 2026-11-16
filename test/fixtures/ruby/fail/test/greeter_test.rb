# frozen_string_literal: true

require 'test_helper'

class GreeterTest < Minitest::Test
  def test_skipped
    skip
    assert_equal 1, Greeter.run('1')
  end
end
