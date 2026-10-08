package bio.terra.app.controller;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyBoolean;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.ArgumentMatchers.isNull;
import static org.mockito.Mockito.when;

import bio.terra.app.configuration.ApplicationConfiguration;
import bio.terra.common.category.Unit;
import bio.terra.common.exception.UnauthorizedException;
import bio.terra.common.fixtures.AuthenticationFixtures;
import bio.terra.common.iam.AuthenticatedUserRequestFactory;
import bio.terra.common.iam.BearerTokenFactory;
import bio.terra.model.DRSAccessMethod;
import bio.terra.model.DRSAccessMethod.TypeEnum;
import bio.terra.model.DRSAccessURL;
import bio.terra.model.DRSChecksum;
import bio.terra.model.DRSObject;
import bio.terra.service.filedata.DrsService;
import jakarta.servlet.http.HttpServletRequest;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.time.Duration;
import java.util.List;
import java.util.zip.CRC32C;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.SpringBootConfiguration;
import org.springframework.boot.autoconfigure.EnableAutoConfiguration;
import org.springframework.boot.autoconfigure.jdbc.DataSourceAutoConfiguration;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.context.SpringBootTest.WebEnvironment;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.context.annotation.Import;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.context.bean.override.mockito.MockitoBean;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

/**
 * A real HTTP harness for git-drs Terra DRS integration tests. The TDR API controller and its
 * generated API mapping are real; storage and authentication are stubbed. The test stays alive
 * until the CI driver creates GIT_DRS_TDR_STOP_FILE.
 */
@SpringBootTest(
    classes = TerraDrsHarnessTest.HarnessApplication.class,
    webEnvironment = WebEnvironment.RANDOM_PORT)
@ActiveProfiles({"google", "unittest"})
@Tag(Unit.TAG)
class TerraDrsHarnessTest {
  private static final String OBJECT_ID = "v2_4f770147-e372-339b-b9fa-0a7a83cf30cf";
  private static final String ACCESS_ID = "https-access";
  private static final byte[] FIXTURE_BYTES =
      "git-drs Terra DRS HTTP integration fixture\n".getBytes(java.nio.charset.StandardCharsets.UTF_8);
  private static final Duration STOP_TIMEOUT = Duration.ofMinutes(2);

  @LocalServerPort private int port;

  @MockitoBean private ApplicationConfiguration applicationConfiguration;
  @MockitoBean private DrsService drsService;
  @MockitoBean private AuthenticatedUserRequestFactory authenticatedUserRequestFactory;
  @MockitoBean private BearerTokenFactory bearerTokenFactory;

  @BeforeEach
  void configureFixtureAndSignalReady() throws Exception {
    Path portFile = requiredPath("GIT_DRS_TDR_PORT_FILE");
    Path stopFile = requiredPath("GIT_DRS_TDR_STOP_FILE");
    Files.deleteIfExists(portFile);
    Files.deleteIfExists(stopFile);
    createParentDirectories(portFile);
    createParentDirectories(stopFile);

    var testUser = AuthenticationFixtures.randomUserRequest();
    when(authenticatedUserRequestFactory.from(any()))
        .thenAnswer(
            invocation -> {
              HttpServletRequest request = invocation.getArgument(0);
              if (!"Bearer tdr-ci-fixture".equals(request.getHeader("Authorization"))) {
                throw new UnauthorizedException("Invalid fixture bearer token");
              }
              return testUser;
            });
    when(drsService.lookupObjectByDrsId(any(), eq(OBJECT_ID), anyBoolean()))
        .thenReturn(fixtureObject());
    when(drsService.getAccessUrlForObjectId(any(), eq(OBJECT_ID), eq(ACCESS_ID), isNull()))
        .thenReturn(new DRSAccessURL().url("http://127.0.0.1:" + port + "/fixture-data"));

    Files.writeString(portFile, Integer.toString(port));
  }

  @Test
  void serveTdrApiUntilCiDriverStops() throws Exception {
    Path stopFile = requiredPath("GIT_DRS_TDR_STOP_FILE");
    long deadline = System.nanoTime() + STOP_TIMEOUT.toNanos();
    while (!Files.exists(stopFile) && System.nanoTime() < deadline) {
      Thread.sleep(200);
    }
    if (!Files.exists(stopFile)) {
      throw new IllegalStateException(
          "Timed out after " + STOP_TIMEOUT + " waiting for " + stopFile);
    }
  }

  private static DRSObject fixtureObject() throws NoSuchAlgorithmException {
    CRC32C crc32c = new CRC32C();
    crc32c.update(FIXTURE_BYTES, 0, FIXTURE_BYTES.length);
    String crcHex = String.format("%08x", crc32c.getValue());
    String md5Hex =
        java.util.HexFormat.of().formatHex(MessageDigest.getInstance("MD5").digest(FIXTURE_BYTES));

    return new DRSObject()
        .id(OBJECT_ID)
        .selfUri("drs://localhost/" + OBJECT_ID)
        .size((long) FIXTURE_BYTES.length)
        .checksums(
            List.of(
                new DRSChecksum().type("crc32c").checksum(crcHex),
                new DRSChecksum().type("md5").checksum(md5Hex)))
        .accessMethods(List.of(new DRSAccessMethod().type(TypeEnum.HTTPS).accessId(ACCESS_ID)));
  }

  private static Path requiredPath(String envName) {
    String value = System.getenv(envName);
    if (value == null || value.isBlank()) {
      throw new IllegalStateException(envName + " must name a coordination file");
    }
    return Path.of(value);
  }

  private static void createParentDirectories(Path path) throws IOException {
    Path parent = path.toAbsolutePath().getParent();
    if (parent != null) {
      Files.createDirectories(parent);
    }
  }

  @SpringBootConfiguration
  @EnableAutoConfiguration(exclude = DataSourceAutoConfiguration.class)
  @Import({DataRepositoryServiceApiController.class, FixtureDataController.class})
  static class HarnessApplication {}

  @RestController
  static class FixtureDataController {
    @GetMapping("/fixture-data")
    ResponseEntity<byte[]> fixtureData() {
      return ResponseEntity.ok()
          .contentType(MediaType.APPLICATION_OCTET_STREAM)
          .body(FIXTURE_BYTES);
    }
  }
}
