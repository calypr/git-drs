package bio.terra.service.filedata;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyInt;
import static org.mockito.ArgumentMatchers.anyLong;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import bio.terra.app.configuration.ApplicationConfiguration;
import bio.terra.app.configuration.DrsConfiguration;
import bio.terra.app.configuration.EcmConfiguration;
import bio.terra.app.controller.DataRepositoryServiceApiController;
import bio.terra.app.logging.PerformanceLogger;
import bio.terra.app.usermetrics.UserLoggingMetrics;
import bio.terra.common.category.Unit;
import bio.terra.common.exception.UnauthorizedException;
import bio.terra.common.fixtures.AuthenticationFixtures;
import bio.terra.common.iam.AuthenticatedUserRequestFactory;
import bio.terra.common.iam.BearerTokenFactory;
import bio.terra.model.BillingProfileModel;
import bio.terra.model.CloudPlatform;
import bio.terra.service.auth.iam.IamService;
import bio.terra.service.dataset.Dataset;
import bio.terra.service.dataset.DatasetSummary;
import bio.terra.service.filedata.google.gcs.GcsProjectFactory;
import bio.terra.service.job.JobService;
import bio.terra.service.resourcemanagement.ResourceService;
import bio.terra.service.resourcemanagement.google.GoogleProjectResource;
import bio.terra.service.snapshot.Snapshot;
import bio.terra.service.snapshot.SnapshotProject;
import bio.terra.service.snapshot.SnapshotService;
import bio.terra.service.snapshot.SnapshotSource;
import com.google.cloud.storage.BlobInfo;
import com.google.cloud.storage.Bucket;
import com.google.cloud.storage.Storage;
import com.google.cloud.storage.Storage.BucketGetOption;
import jakarta.servlet.http.HttpServletRequest;
import java.io.IOException;
import java.net.URL;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.UUID;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.springframework.boot.SpringBootConfiguration;
import org.springframework.boot.autoconfigure.EnableAutoConfiguration;
import org.springframework.boot.autoconfigure.jdbc.DataSourceAutoConfiguration;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.context.SpringBootTest.WebEnvironment;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.context.annotation.Import;
import org.springframework.core.task.AsyncTaskExecutor;
import org.springframework.core.task.SimpleAsyncTaskExecutor;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.context.bean.override.mockito.MockitoBean;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

/**
 * A real HTTP harness for git-drs Terra DRS integration tests. The TDR API controller, generated
 * API mapping, and DrsService are real. Snapshot lookup, authorization, file metadata, URL signing,
 * and authentication are fixture-backed. The test stays alive until the CI driver creates
 * GIT_DRS_TDR_STOP_FILE.
 */
@SpringBootTest(
    classes = TerraDrsHarnessTest.HarnessApplication.class,
    webEnvironment = WebEnvironment.RANDOM_PORT,
    properties = "spring.cloud.gcp.firestore.enabled=false")
@ActiveProfiles({"google", "unittest"})
@Tag(Unit.TAG)
class TerraDrsHarnessTest {
  private static final String OBJECT_ID = "v2_4f770147-e372-339b-b9fa-0a7a83cf30cf";
  private static final UUID SNAPSHOT_ID = UUID.fromString("11111111-2222-4333-8444-555555555555");
  private static final UUID FILE_ID = UUID.fromString("4f770147-e372-339b-b9fa-0a7a83cf30cf");
  private static final byte[] FIXTURE_BYTES =
      "git-drs Terra DRS HTTP integration fixture\n"
          .getBytes(java.nio.charset.StandardCharsets.UTF_8);
  private static final Duration STOP_TIMEOUT = Duration.ofMinutes(20);
  private static final AtomicInteger fixtureDataRequests = new AtomicInteger();

  @LocalServerPort private int port;

  @MockitoBean private ApplicationConfiguration applicationConfiguration;
  @MockitoBean private SnapshotService snapshotService;
  @MockitoBean private FileService fileService;
  @MockitoBean private DrsDao drsDao;
  @MockitoBean private IamService iamService;
  @MockitoBean private ResourceService resourceService;
  @MockitoBean private DrsConfiguration drsConfiguration;
  @MockitoBean private JobService jobService;
  @MockitoBean private PerformanceLogger performanceLogger;
  @MockitoBean private GcsProjectFactory gcsProjectFactory;
  @MockitoBean private EcmConfiguration ecmConfiguration;
  @MockitoBean private DrsMetricsService drsMetricsService;
  @MockitoBean private UserLoggingMetrics userLoggingMetrics;
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

    fixtureDataRequests.set(0);
    var testUser = AuthenticationFixtures.randomUserRequest();
    when(authenticatedUserRequestFactory.from(any()))
        .thenAnswer(
            invocation -> {
              HttpServletRequest request = invocation.getArgument(0);
              if ("/fixture-data".equals(request.getRequestURI())) {
                return testUser;
              }
              if (!"Bearer tdr-ci-fixture".equals(request.getHeader("Authorization"))) {
                throw new UnauthorizedException("Invalid fixture bearer token");
              }
              return testUser;
            });
    when(applicationConfiguration.getDnsName()).thenReturn("drs.anv0");
    when(drsConfiguration.maxDrsLookups()).thenReturn(10);
    when(jobService.getActivePodCount()).thenReturn(1);
    when(snapshotService.retrieve(SNAPSHOT_ID)).thenReturn(fixtureSnapshot());
    when(snapshotService.retrieveSnapshotProject(SNAPSHOT_ID)).thenReturn(new SnapshotProject());
    when(drsDao.retrieveReferencedSnapshotIds(any())).thenReturn(java.util.List.of(SNAPSHOT_ID));
    when(fileService.lookupSnapshotFSItem(any(), any(), anyInt())).thenReturn(fixtureFile());

    Storage storage = mock(Storage.class);
    Bucket bucket = mock(Bucket.class);
    when(bucket.getLocation()).thenReturn("us-central1");
    when(storage.get(eq("fixture-bucket"), any(BucketGetOption[].class))).thenReturn(bucket);
    doReturn(new URL("http://127.0.0.1:" + port + "/fixture-data"))
        .when(storage)
        .signUrl(
            any(BlobInfo.class),
            anyLong(),
            any(TimeUnit.class),
            any(Storage.SignUrlOption[].class));
    when(gcsProjectFactory.getStorage(any())).thenReturn(storage);

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

  private static Snapshot fixtureSnapshot() {
    BillingProfileModel billingProfile =
        new BillingProfileModel().id(UUID.fromString("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"));
    return new Snapshot()
        .id(SNAPSHOT_ID)
        .profileId(billingProfile.getId())
        .globalFileIds(true)
        .projectResource(new GoogleProjectResource().googleProjectId("fixture-snapshot-project"))
        .snapshotSources(
            java.util.List.of(
                new SnapshotSource()
                    .dataset(
                        new Dataset(
                                new DatasetSummary()
                                    .selfHosted(true)
                                    .defaultProfileId(billingProfile.getId())
                                    .cloudPlatform(CloudPlatform.GCP)
                                    .billingProfiles(java.util.List.of(billingProfile)))
                            .id(UUID.fromString("bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"))
                            .name("git-drs-ci-fixture")
                            .projectResource(
                                new GoogleProjectResource()
                                    .googleProjectId("fixture-dataset-project")))));
  }

  private static FSFile fixtureFile() {
    java.util.zip.CRC32C crc32c = new java.util.zip.CRC32C();
    crc32c.update(FIXTURE_BYTES, 0, FIXTURE_BYTES.length);
    String crcHex = String.format("%08x", crc32c.getValue());
    String md5Hex;
    try {
      md5Hex =
          java.util.HexFormat.of()
              .formatHex(java.security.MessageDigest.getInstance("MD5").digest(FIXTURE_BYTES));
    } catch (java.security.NoSuchAlgorithmException e) {
      throw new IllegalStateException(e);
    }
    return new FSFile()
        .fileId(FILE_ID)
        .path("1614321.merge_output.gvcf.gz")
        .cloudPath("gs://fixture-bucket/1614321.merge_output.gvcf.gz")
        .cloudPlatform(CloudPlatform.GCP)
        .bucketResourceId("fixture-bucket-resource")
        .createdDate(java.time.Instant.parse("2026-10-07T00:00:00Z"))
        .size((long) FIXTURE_BYTES.length)
        .checksumCrc32c(crcHex)
        .checksumMd5(md5Hex);
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
  @Import({
    DataRepositoryServiceApiController.class,
    FixtureDataController.class,
    DrsIdService.class,
    DrsService.class
  })
  static class HarnessApplication {
    @org.springframework.context.annotation.Bean(name = "drsResolutionThreadpool")
    AsyncTaskExecutor drsResolutionThreadpool() {
      return new SimpleAsyncTaskExecutor("drs-resolution-test-");
    }
  }

  @RestController
  static class FixtureDataController {
    @GetMapping("/fixture-data")
    ResponseEntity<byte[]> fixtureData() {
      fixtureDataRequests.incrementAndGet();
      return ResponseEntity.ok()
          .contentType(MediaType.APPLICATION_OCTET_STREAM)
          .body(FIXTURE_BYTES);
    }

    @GetMapping("/fixture-data-requests")
    int fixtureDataRequestCount() {
      return fixtureDataRequests.get();
    }
  }
}
