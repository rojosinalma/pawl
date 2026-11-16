# frozen_string_literal: true

require 'net/http'

# One violation per catalog rule.
module Greeter
  CACHE = {}

  module_function

  def swallow
    yield
  rescue => e
  end

  def debug(value)
    binding.irb
    value
  end

  def getValue(value)
    unused = value * 2
    value
  end

  def api_base
    ENV['ACME_API_BASE']
  end

  def test_env?
    ENV['RACK_ENV'] == 'test'
  end

  def download(uri)
    Net::HTTP.get(URI(uri))
  end

  def run(code)
    eval(code)
  end

  def badly_indented
      1
  end

  def classify(a, b, c)
    return :a if a && b
    return :b if b || c
    return :c if a && c
    return :d if a || b
    return :e if b && c
    return :f if c || a
    return :g if a == 1
    return :h if b == 2
    return :i if c == 3
    return :j if a == b
    return :k if b == c
    return :l if c == a
    return :m if a.nil?
    return :n if b.nil?
    return :o if c.nil?
    return :p if a.zero?

    :q
  end
end
