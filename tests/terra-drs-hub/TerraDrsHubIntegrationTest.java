package bio.terra.drshub.controllers;

import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.test.context.ActiveProfiles;

@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
@ActiveProfiles({"test", "human-readable-logging", "hub-ci"})
@Tag("Unit")
class TerraDrsHubIntegrationTest {
  private static final Duration STOP_TIMEOUT = Duration.ofMinutes(20);

  @LocalServerPort private int port;

  @Test
  void serveHubUntilGitDrsPullFinishes() throws Exception {
    Path portFile = requiredPath("GIT_DRS_HUB_PORT_FILE");
    Path stopFile = requiredPath("GIT_DRS_HUB_STOP_FILE");
    Files.deleteIfExists(stopFile);
    Files.writeString(portFile, Integer.toString(port));

    long deadline = System.nanoTime() + STOP_TIMEOUT.toNanos();
    while (!Files.exists(stopFile) && System.nanoTime() < deadline) {
      Thread.sleep(200);
    }
    if (!Files.exists(stopFile)) {
      throw new IllegalStateException("Timed out waiting for git-drs pull to finish");
    }
  }

  private static Path requiredPath(String envName) {
    String value = System.getenv(envName);
    if (value == null || value.isBlank()) {
      throw new IllegalStateException(envName + " must name a coordination file");
    }
    return Path.of(value);
  }
}
