// Dev-only entry for fixture.html: install the fake Bridge, then boot the real app.
import { installFixtureBackend } from './backend'
import { scenarioFromSearch } from './scenarios'

installFixtureBackend(scenarioFromSearch(location.search))
await import('../main')
