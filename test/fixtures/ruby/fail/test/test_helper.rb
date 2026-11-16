# frozen_string_literal: true

if ENV['COVERAGE']
  require 'simplecov'
  require 'simplecov-lcov'
  SimpleCov::Formatter::LcovFormatter.config.report_with_single_file = true
  SimpleCov::Formatter::LcovFormatter.config.single_report_path = '.pawl/reports/ruby-lcov.info'
  SimpleCov.formatter = SimpleCov::Formatter::LcovFormatter
  SimpleCov.start { add_filter '/test/' }
end

require 'minitest/autorun'
require 'greeter'
