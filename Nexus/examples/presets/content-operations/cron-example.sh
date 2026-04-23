#!/bin/bash
# Example cron schedule for content operations agents
# Each agent is an isolated process that connects to Nexus via HTTP

# Every 30 minutes: check competitors
*/30 * * * * /path/to/competitor-monitor.sh

# Every hour: aggregate RSS
0 * * * * /path/to/rss-aggregator.sh

# Every 2 hours: create content
0 */2 * * * /path/to/content-creator.sh

# Every 4 hours: review pending drafts
0 */4 * * * /path/to/editorial-review.sh

# 3x daily: social media scheduling
0 8,14,20 * * * /path/to/social-media.sh
