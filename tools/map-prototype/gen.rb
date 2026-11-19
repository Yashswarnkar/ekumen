# Builds the Ekumen build map from the repo's own files.
# Usage: ruby gen.rb <repo-root> <template.html> <out.html>
# Deterministic: reads .ekumen/registry.yaml, .ekumen/decisions/*.yaml and `gh pr list`; no judgment here.
require "yaml"
require "json"
require "date"
require "time"

root, tpl, out = ARGV
ek = File.join(root, ".ekumen")

registry = YAML.load_file(File.join(ek, "registry.yaml"))
decisions = Dir[File.join(ek, "decisions", "*.yaml")].sort.map do |f|
  YAML.load_file(f)
end

prs = JSON.parse(`gh pr list --repo Yashswarnkar/ekumen --state all --limit 100 --json number,title,headRefName,isDraft,state,url,statusCheckRollup`)
prs = prs.map do |p|
  checks = (p["statusCheckRollup"] || []).map { |c| c["conclusion"] || c["status"] }.compact.uniq
  { number: p["number"], title: p["title"], branch: p["headRefName"], draft: p["isDraft"],
    state: p["state"], url: p["url"], checks: checks }
end

commit = `git -C #{root} rev-parse --short HEAD`.strip
data = {
  generated_at: Time.now.utc.iso8601,
  commit: commit,
  registry: registry,
  decisions: decisions,
  prs: prs,
}
json = JSON.generate(JSON.parse(data.to_json)) # normalizes Date objects to strings
json = json.gsub("</", "<\\/")
html = File.read(tpl).sub("/*DATA*/null", json)
File.write(out, html)
puts "wrote #{out}: #{registry['tasks'].size} tasks, #{decisions.size} decisions, #{prs.size} PRs @ #{commit}"
