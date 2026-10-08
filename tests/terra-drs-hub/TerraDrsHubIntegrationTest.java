package bio.terra.drshub.controllers;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.List;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.test.context.ActiveProfiles;

@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
@ActiveProfiles({"test", "human-readable-logging", "hub-ci"})
@Tag("Unit")
class TerraDrsHubIntegrationTest {
  private static final String OBJECT_ID = "v2_4f770147-e372-339b-b9fa-0a7a83cf30cf";

  @LocalServerPort private int port;
  @Autowired private ObjectMapper objectMapper;

  @Test
  void resolvesAnvilCompactIdThroughHubAndTheLiveTdrDrsApi() throws Exception {
    Path proxyLog = Path.of(requiredEnv("GIT_DRS_TDR_PROXY_LOG"));
    Files.deleteIfExists(proxyLog);

    String requestBody =
        objectMapper.writeValueAsString(
            java.util.Map.of(
                "url",
                "drs://drs.anv0:" + OBJECT_ID,
                "fields",
                List.of("size", "fileName", "hashes", "accessUrl")));
    HttpRequest request =
        HttpRequest.newBuilder()
            .uri(URI.create("http://127.0.0.1:" + port + "/api/v4/drs/resolve"))
            .timeout(Duration.ofSeconds(30))
            .header("Authorization", "Bearer tdr-ci-fixture")
            .header("Content-Type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString(requestBody))
            .build();

    HttpResponse<String> response =
        HttpClient.newHttpClient().send(request, HttpResponse.BodyHandlers.ofString());
    assertEquals(200, response.statusCode(), response.body());

    JsonNode metadata = objectMapper.readTree(response.body());
    assertEquals(43, metadata.path("size").asLong());
    assertEquals("1614321.merge_output.gvcf.gz", metadata.path("fileName").asText());
    assertTrue(metadata.path("hashes").isObject(), response.body());
    assertEquals("d0147894", metadata.path("hashes").path("crc32c").asText());
    assertEquals("e3fbcfb74b00bf57af80060c0ac10d03", metadata.path("hashes").path("md5").asText());
    assertEquals(
        "http://127.0.0.1:" + requiredEnv("GIT_DRS_TDR_HTTP_PORT") + "/fixture-data",
        metadata.path("accessUrl").path("url").asText());

    List<JsonNode> proxiedRequests =
        Files.readAllLines(proxyLog).stream().map(this::readJson).toList();
    assertTrue(
        proxiedRequests.stream()
            .anyMatch(
                entry ->
                    entry.path("method").asText().equals("OPTIONS")
                        && entry.path("path").asText().endsWith("/objects/" + OBJECT_ID)),
        proxiedRequests.toString());
    assertTrue(
        proxiedRequests.stream()
            .anyMatch(
                entry ->
                    entry.path("method").asText().equals("GET")
                        && entry.path("path").asText().endsWith("/objects/" + OBJECT_ID)),
        proxiedRequests.toString());
    assertTrue(
        proxiedRequests.stream()
            .anyMatch(
                entry ->
                    entry.path("method").asText().equals("GET")
                        && entry
                            .path("path")
                            .asText()
                            .contains("/objects/" + OBJECT_ID + "/access/")),
        proxiedRequests.toString());
    assertTrue(
        proxiedRequests.stream()
            .filter(entry -> entry.path("method").asText().equals("GET"))
            .allMatch(
                entry -> entry.path("authorization").asText().equals("Bearer tdr-ci-fixture")),
        proxiedRequests.toString());
  }

  private JsonNode readJson(String line) {
    try {
      return objectMapper.readTree(line);
    } catch (Exception e) {
      throw new IllegalStateException("Invalid TDR proxy log entry: " + line, e);
    }
  }

  private static String requiredEnv(String name) {
    String value = System.getenv(name);
    if (value == null || value.isBlank()) {
      throw new IllegalStateException(name + " must be set by the CI driver");
    }
    return value;
  }
}
