# Read Crew's public file contract; never hand-maintain a second roster.
require 'yaml'
require 'json'
require 'digest'
require 'date'
require 'time'
source, output = ARGV
abort 'usage: ruby import-communication-roster.rb <seats.yaml> <output.json>' unless source && output
raw = File.read(source)
doc = YAML.safe_load(raw, permitted_classes: [Date], aliases: false)
abort 'unsupported Crew roster version' unless doc['version'] == 1
members = {}
%w[seats temporary humans outside_seats].each do |section|
  (doc[section] || []).each do |seat|
    next if seat['ended']
    id = seat['session'] ? "thread:#{seat['session']}" : (seat['kind'] == 'human' || section == 'humans' ? seat['id'] : nil)
    next unless id
    member = members[id] ||= {id: id, label: seat['name'] || seat['id'], memberships: []}
    ([seat['domain_id']] + (seat['also_domain_ids'] || [])).uniq.each do |domain|
      member[:memberships] << {seat: seat['id'], organization: 'Platform', domain: domain, role: seat['kind'] || (section == 'humans' ? 'human' : 'outside'), invitedBy: seat['invited_by']}.reject { |key, value| key == :invitedBy && value.nil? }
    end
  end
end
File.write(output, JSON.pretty_generate({version: 1, source: 'bifrost-trade-infra:agent-config/charters/seats.yaml', sha256: Digest::SHA256.hexdigest(raw), importedAt: Time.now.utc.iso8601, members: members.values}) + "\n")
