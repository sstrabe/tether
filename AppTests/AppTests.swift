import Testing
@testable import Tether

@Test func settingsRoundTripThroughProviderConfiguration() {
    var s = TetherSettings()
    s.controlURL = "https://control.example"
    s.hostname = "phone"
    s.exitNode = "100.64.0.1"
    let back = TetherSettings(providerConfiguration: s.providerConfiguration)
    #expect(back == s)
}

@Test func statusDecodesCoreJSON() throws {
    let json = """
    {"state":"Running","self":{"name":"phone","ips":["100.64.0.5"],"online":true,"rxBytes":0,"txBytes":0},
     "peers":[{"name":"laptop","dnsName":"laptop.example.ts.net.","ips":["100.64.0.6"],"online":true,"relay":"vps","rxBytes":1,"txBytes":2}],
     "health":[],"uptimeSeconds":12.5,
     "counters":{"pathUpdates":40,"pathChanges":3,"settingsApplied":2,"settingsSkipped":9},
     "memory":{"heapMB":8.4,"sysMB":23.9,"limitMB":30,"numGC":4},"time":"2026-11-24T12:00:00Z"}
    """
    let s = try JSONDecoder().decode(CoreStatus.self, from: Data(json.utf8))
    #expect(s.peers.first?.shortName == "laptop")
    #expect(s.peers.first?.isDirect == false)
    #expect(s.counters?.pathChanges == 3)
}
