package bio.terra.service.filedata;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.when;

import bio.terra.app.configuration.ApplicationConfiguration;
import bio.terra.app.configuration.DataRepoJdbcConfiguration;
import bio.terra.app.configuration.DrsConfiguration;
import bio.terra.app.configuration.EcmConfiguration;
import bio.terra.app.controller.DataRepositoryServiceApiController;
import bio.terra.app.logging.PerformanceLogger;
import bio.terra.app.usermetrics.UserLoggingMetrics;
import bio.terra.common.EmbeddedDatabaseTest;
import bio.terra.common.category.Unit;
import bio.terra.common.exception.UnauthorizedException;
import bio.terra.common.fixtures.AuthenticationFixtures;
import bio.terra.common.iam.AuthenticatedUserRequestFactory;
import bio.terra.common.iam.BearerTokenFactory;
import bio.terra.model.BillingProfileModel;
import bio.terra.model.CloudPlatform;
import bio.terra.service.auth.iam.IamService;
import bio.terra.service.auth.ras.EcmService;
import bio.terra.service.configuration.ConfigEnum;
import bio.terra.service.configuration.ConfigurationService;
import bio.terra.service.dataset.AssetDao;
import bio.terra.service.dataset.Dataset;
import bio.terra.service.dataset.DatasetDao;
import bio.terra.service.dataset.DatasetRelationshipDao;
import bio.terra.service.dataset.DatasetService;
import bio.terra.service.dataset.DatasetSummary;
import bio.terra.service.dataset.DatasetTableDao;
import bio.terra.service.dataset.StorageResourceDao;
import bio.terra.service.duos.DuosClient;
import bio.terra.service.duos.DuosDao;
import bio.terra.service.filedata.google.firestore.EncodeFixture;
import bio.terra.service.filedata.google.firestore.FireStoreDao;
import bio.terra.service.filedata.google.firestore.FireStoreDependencyDao;
import bio.terra.service.filedata.google.firestore.FireStoreDirectoryEntry;
import bio.terra.service.filedata.google.firestore.FireStoreFile;
import bio.terra.service.filedata.google.gcs.GcsProjectFactory;
import bio.terra.service.job.JobService;
import bio.terra.service.journal.JournalService;
import bio.terra.service.load.LoadService;
import bio.terra.service.profile.ProfileService;
import bio.terra.service.rawls.RawlsService;
import bio.terra.service.resourcemanagement.MetadataDataAccessUtils;
import bio.terra.service.resourcemanagement.ResourceService;
import bio.terra.service.resourcemanagement.google.GoogleProjectResource;
import bio.terra.service.snapshot.Snapshot;
import bio.terra.service.snapshot.SnapshotDao;
import bio.terra.service.snapshot.SnapshotMapTableDao;
import bio.terra.service.snapshot.SnapshotRelationshipDao;
import bio.terra.service.snapshot.SnapshotService;
import bio.terra.service.snapshot.SnapshotSource;
import bio.terra.service.snapshot.SnapshotTableDao;
import bio.terra.service.snapshotbuilder.SnapshotBuilderSettingsDao;
import bio.terra.service.snapshotbuilder.SnapshotRequestDao;
import bio.terra.service.tabulardata.google.bigquery.BigQuerySnapshotPdao;
import com.fasterxml.jackson.databind.MapperFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.datatype.jdk8.Jdk8Module;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import com.fasterxml.jackson.module.paramnames.ParameterNamesModule;
import com.google.auth.oauth2.ServiceAccountCredentials;
import com.google.cloud.storage.Storage;
import com.google.cloud.storage.StorageOptions;
import com.sun.net.httpserver.HttpServer;
import jakarta.servlet.http.HttpServletRequest;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.URI;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.KeyPairGenerator;
import java.time.Duration;
import java.util.Base64;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import javax.sql.DataSource;
import liquibase.Contexts;
import liquibase.Liquibase;
import liquibase.database.jvm.JdbcConnection;
import liquibase.resource.ClassLoaderResourceAccessor;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.boot.SpringBootConfiguration;
import org.springframework.boot.autoconfigure.EnableAutoConfiguration;
import org.springframework.boot.autoconfigure.jdbc.DataSourceAutoConfiguration;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.context.SpringBootTest.WebEnvironment;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.context.annotation.ComponentScan;
import org.springframework.context.annotation.FilterType;
import org.springframework.context.annotation.Import;
import org.springframework.core.task.AsyncTaskExecutor;
import org.springframework.core.task.SimpleAsyncTaskExecutor;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.context.bean.override.mockito.MockitoBean;

/**
 * A real HTTP harness for git-drs Terra DRS integration tests. The TDR API controller, generated
 * API mapping, DrsService, SnapshotService, SnapshotDao, DatasetDao, FireStoreDao, and the GCS
 * client are real. The test stays alive until the CI driver creates GIT_DRS_TDR_STOP_FILE.
 */
@SpringBootTest(
    classes = TerraDrsHarnessTest.HarnessApplication.class,
    webEnvironment = WebEnvironment.RANDOM_PORT,
    properties = "spring.cloud.gcp.firestore.enabled=false")
@ActiveProfiles({"google", "unittest"})
@Tag(Unit.TAG)
@EmbeddedDatabaseTest
class TerraDrsHarnessTest {
  private static final String OBJECT_ID = "v2_4f770147-e372-339b-b9fa-0a7a83cf30cf";
  private static final UUID SNAPSHOT_ID = UUID.fromString("11111111-2222-4333-8444-555555555555");
  private static final UUID FILE_ID = UUID.fromString("4f770147-e372-339b-b9fa-0a7a83cf30cf");
  private static final UUID GREGOR_1614321_FILE_ID =
      UUID.fromString("c5ae75de-1f5c-3d40-bcd9-02f827fbf2d3");
  private static final UUID GREGOR_1614322_FILE_ID =
      UUID.fromString("4872d195-f80a-3662-90a7-2aa90e0a7f53");
  private static final UUID BILLING_PROFILE_ID =
      UUID.fromString("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee");
  private static final UUID DATASET_ID = UUID.fromString("bbbbbbbb-cccc-4ddd-8eee-ffffffffffff");
  private static final UUID SNAPSHOT_PROJECT_RESOURCE_ID =
      UUID.fromString("cccccccc-dddd-4eee-8fff-000000000001");
  private static final UUID DATASET_PROJECT_RESOURCE_ID =
      UUID.fromString("cccccccc-dddd-4eee-8fff-000000000002");
  private static final byte[] FIXTURE_BYTES =
      "git-drs Terra DRS HTTP integration fixture\n"
          .getBytes(java.nio.charset.StandardCharsets.UTF_8);
  // IDs and sample names are inspired by GREGoR records; these payloads are tiny synthetic text,
  // and each seeded size/checksum reflects the synthetic bytes rather than the public file.
  // Prefixing 1614321 avoids colliding with the original fixture path used by existing scenarios.
  private static final List<FixtureObject> FIXTURE_OBJECTS =
      List.of(
          new FixtureObject(OBJECT_ID, FILE_ID, "1614321.merge_output.gvcf.gz", FIXTURE_BYTES),
          new FixtureObject(
              "v2_c5ae75de-1f5c-3d40-bcd9-02f827fbf2d3",
              GREGOR_1614321_FILE_ID,
              "gregor-1614321.merge_output.gvcf.gz",
              "synthetic GREGoR 1614321 fixture; not genomic data\n"
                  .getBytes(java.nio.charset.StandardCharsets.UTF_8)),
          new FixtureObject(
              "v2_4872d195-f80a-3662-90a7-2aa90e0a7f53",
              GREGOR_1614322_FILE_ID,
              "1614322.merge_output.gvcf.gz",
              "synthetic GREGoR 1614322 fixture; not genomic data\n"
                  .getBytes(java.nio.charset.StandardCharsets.UTF_8)));
  private static final Duration STOP_TIMEOUT = Duration.ofMinutes(20);
  @LocalServerPort private int port;
  private HttpServer tokenServer;

  @MockitoBean private IamService iamService;
  @MockitoBean private ResourceService resourceService;
  @MockitoBean private ConfigurationService configurationService;
  @MockitoBean private DatasetService datasetService;
  @MockitoBean private LoadService loadService;
  @MockitoBean private ProfileService profileService;
  @MockitoBean private JournalService journalService;
  @MockitoBean private DuosDao duosDao;
  @MockitoBean private FireStoreDependencyDao dependencyDao;
  @MockitoBean private BigQuerySnapshotPdao bigQuerySnapshotPdao;
  @MockitoBean private SnapshotRequestDao snapshotRequestDao;
  @MockitoBean private SnapshotRelationshipDao snapshotRelationshipDao;
  @MockitoBean private MetadataDataAccessUtils metadataDataAccessUtils;
  @MockitoBean private EcmService ecmService;
  @MockitoBean private RawlsService rawlsService;
  @MockitoBean private DuosClient duosClient;
  @MockitoBean private SnapshotBuilderSettingsDao snapshotBuilderSettingsDao;
  @MockitoBean private DataRepoJdbcConfiguration dataRepoJdbcConfiguration;
  @MockitoBean private DrsConfiguration drsConfiguration;
  @MockitoBean private JobService jobService;
  @MockitoBean private PerformanceLogger performanceLogger;
  @MockitoBean private GcsProjectFactory gcsProjectFactory;
  @MockitoBean private EcmConfiguration ecmConfiguration;
  @MockitoBean private DrsMetricsService drsMetricsService;
  @MockitoBean private UserLoggingMetrics userLoggingMetrics;
  @MockitoBean private AuthenticatedUserRequestFactory authenticatedUserRequestFactory;
  @MockitoBean private BearerTokenFactory bearerTokenFactory;
  @Autowired private DrsDao drsDao;
  @Autowired private DrsIdService drsIdService;
  @Autowired private FireStoreDao fireStoreDao;
  @Autowired private JdbcTemplate jdbcTemplate;

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
    when(drsConfiguration.maxDrsLookups()).thenReturn(10);
    when(jobService.getActivePodCount()).thenReturn(1);
    when(resourceService.getProjectResource(SNAPSHOT_PROJECT_RESOURCE_ID))
        .thenReturn(
            new GoogleProjectResource()
                .id(SNAPSHOT_PROJECT_RESOURCE_ID)
                .googleProjectId("fixture-snapshot-project"));
    when(resourceService.getProjectResource(DATASET_PROJECT_RESOURCE_ID))
        .thenReturn(
            new GoogleProjectResource()
                .id(DATASET_PROJECT_RESOURCE_ID)
                .googleProjectId("fixture-dataset-project"));
    when(configurationService.<Integer>getParameterValue(any()))
        .thenAnswer(
            invocation ->
                switch (invocation.getArgument(0, ConfigEnum.class)) {
                  case FIRESTORE_RETRIES -> 1;
                  default -> 100;
                });
    migrateAndSeedDrsRecord();
    seedFirestoreFile();

    tokenServer = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
    tokenServer.createContext(
        "/token",
        exchange -> {
          byte[] response =
              "{\"access_token\":\"tdr-ci-token\",\"expires_in\":3600,\"token_type\":\"Bearer\"}"
                  .getBytes(java.nio.charset.StandardCharsets.UTF_8);
          exchange.getResponseHeaders().set("Content-Type", "application/json");
          exchange.sendResponseHeaders(200, response.length);
          exchange.getResponseBody().write(response);
          exchange.close();
        });
    tokenServer.start();
    var keyPairGenerator = KeyPairGenerator.getInstance("RSA");
    keyPairGenerator.initialize(2048);
    var signingKeyPair = keyPairGenerator.generateKeyPair();
    String privateKeyPem =
        "-----BEGIN PRIVATE KEY-----\n"
            + Base64.getMimeEncoder(64, new byte[] {'\n'})
                .encodeToString(signingKeyPair.getPrivate().getEncoded())
            + "\n-----END PRIVATE KEY-----\n";
    String publicKeyPath = System.getenv("GIT_DRS_TDR_SIGNING_PUBLIC_KEY_FILE");
    if (publicKeyPath == null || publicKeyPath.isBlank()) {
      throw new IllegalStateException("GIT_DRS_TDR_SIGNING_PUBLIC_KEY_FILE must be set");
    }
    Files.writeString(
        Path.of(publicKeyPath),
        "-----BEGIN PUBLIC KEY-----\n"
            + Base64.getMimeEncoder(64, new byte[] {'\n'})
                .encodeToString(signingKeyPair.getPublic().getEncoded())
            + "\n-----END PUBLIC KEY-----\n");
    var storageCredentials =
        ServiceAccountCredentials.fromPkcs8(
            "git-drs-ci@fixture.invalid",
            "git-drs-ci-key",
            privateKeyPem,
            null,
            List.of("https://www.googleapis.com/auth/devstorage.read_only"),
            null,
            URI.create("http://127.0.0.1:" + tokenServer.getAddress().getPort() + "/token"));
    String storageEndpoint = System.getenv("GIT_DRS_TDR_GCS_ENDPOINT");
    if (storageEndpoint == null || storageEndpoint.isBlank()) {
      throw new IllegalStateException("GIT_DRS_TDR_GCS_ENDPOINT must name the local GCS emulator");
    }
    Storage storage =
        StorageOptions.newBuilder()
            .setProjectId("fixture")
            .setHost(storageEndpoint)
            .setCredentials(storageCredentials)
            .build()
            .getService();
    when(gcsProjectFactory.getStorage(any())).thenReturn(storage);

    Files.writeString(portFile, Integer.toString(port));
  }

  @org.junit.jupiter.api.AfterEach
  void stopFixtureTokenServer() {
    if (tokenServer != null) {
      tokenServer.stop(0);
    }
  }

  private void seedFirestoreFile() throws Exception {
    Snapshot snapshot = fixtureSnapshot();
    Dataset dataset = snapshot.getFirstSnapshotSource().getDataset();
    for (FixtureObject fixture : FIXTURE_OBJECTS) {
      fireStoreDao.upsertFileMetadata(
          dataset,
          new FireStoreFile()
              .fileId(fixture.fileId().toString())
              .fileCreatedDate("2026-10-07T00:00:00Z")
              .gspath("gs://fixture-bucket/" + fixture.filename())
              .checksumCrc32c(crc32cHex(fixture.bytes()))
              .checksumMd5(md5Hex(fixture.bytes()))
              .size((long) fixture.bytes().length)
              .mimeType("application/gzip")
              .description("Synthetic CI fixture; no real genomic payload")
              .bucketResourceId("fixture-bucket-resource")
              .loadTag("git-drs-ci"));
      fireStoreDao.createDirectoryEntry(
          dataset,
          new FireStoreDirectoryEntry()
              .fileId(fixture.fileId().toString())
              .isFileRef(true)
              .path("/")
              .name(fixture.filename())
              .datasetId(dataset.getId().toString())
              .fileCreatedDate("2026-10-07T00:00:00Z")
              .loadTag("git-drs-ci"));
    }
    fireStoreDao.addFilesToSnapshot(
        dataset,
        snapshot,
        FIXTURE_OBJECTS.stream().map(fixture -> fixture.fileId().toString()).toList());
  }

  private void migrateAndSeedDrsRecord() throws Exception {
    jdbcTemplate.execute("CREATE EXTENSION IF NOT EXISTS pgcrypto");
    try (var connection = jdbcTemplate.getDataSource().getConnection()) {
      new Liquibase(
              "db/changelog.xml", new ClassLoaderResourceAccessor(), new JdbcConnection(connection))
          .update(new Contexts());
    }
    jdbcTemplate.update(
        "INSERT INTO billing_profile (id, name, billing_account_id) VALUES (?, ?, ?)",
        BILLING_PROFILE_ID,
        "git-drs-ci-profile",
        "fixture-billing-account");
    jdbcTemplate.update(
        "INSERT INTO project_resource (id, google_project_id, google_project_number, profile_id) VALUES (?, ?, ?, ?)",
        SNAPSHOT_PROJECT_RESOURCE_ID,
        "fixture-snapshot-project",
        "100000000001",
        BILLING_PROFILE_ID);
    jdbcTemplate.update(
        "INSERT INTO project_resource (id, google_project_id, google_project_number, profile_id) VALUES (?, ?, ?, ?)",
        DATASET_PROJECT_RESOURCE_ID,
        "fixture-dataset-project",
        "100000000002",
        BILLING_PROFILE_ID);
    jdbcTemplate.update(
        "INSERT INTO dataset (id, name, default_profile_id, project_resource_id, self_hosted, sharedlock, tags) VALUES (?, ?, ?, ?, TRUE, '{}', ARRAY[]::TEXT[])",
        DATASET_ID,
        "git-drs-ci-fixture",
        BILLING_PROFILE_ID,
        DATASET_PROJECT_RESOURCE_ID);
    jdbcTemplate.update(
        "INSERT INTO storage_resource (dataset_id, region, cloud_resource, cloud_platform) VALUES (?, ?, ?, ?)",
        DATASET_ID,
        "US_CENTRAL1",
        "FIRESTORE",
        "GCP");
    jdbcTemplate.update(
        "INSERT INTO snapshot (id, name, profile_id, project_resource_id) VALUES (?, ?, ?, ?)",
        SNAPSHOT_ID,
        "git-drs-ci-fixture",
        BILLING_PROFILE_ID,
        SNAPSHOT_PROJECT_RESOURCE_ID);
    jdbcTemplate.update(
        "INSERT INTO snapshot_source (snapshot_id, dataset_id) VALUES (?, ?)",
        SNAPSHOT_ID,
        DATASET_ID);
    List<DrsId> objectIds =
        FIXTURE_OBJECTS.stream()
            .map(fixture -> drsIdService.fromObjectId(fixture.drsId()))
            .toList();
    if (drsDao.recordDrsIdToSnapshot(SNAPSHOT_ID, objectIds) != FIXTURE_OBJECTS.size()) {
      throw new IllegalStateException("TDR DrsDao did not insert all fixture DRS mappings");
    }
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
    BillingProfileModel billingProfile = new BillingProfileModel().id(BILLING_PROFILE_ID);
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

  private static String crc32cHex(byte[] bytes) {
    java.util.zip.CRC32C crc32c = new java.util.zip.CRC32C();
    crc32c.update(bytes, 0, bytes.length);
    return String.format("%08x", crc32c.getValue());
  }

  private static String md5Hex(byte[] bytes) {
    try {
      return java.util.HexFormat.of()
          .formatHex(java.security.MessageDigest.getInstance("MD5").digest(bytes));
    } catch (java.security.NoSuchAlgorithmException e) {
      throw new IllegalStateException(e);
    }
  }

  private record FixtureObject(String drsId, UUID fileId, String filename, byte[] bytes) {}

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
  @ComponentScan(
      basePackages = "bio.terra.service.filedata.google.firestore",
      excludeFilters =
          @ComponentScan.Filter(type = FilterType.ASSIGNABLE_TYPE, classes = EncodeFixture.class))
  @Import({
    DataRepositoryServiceApiController.class,
    DrsIdService.class,
    DrsService.class,
    FileService.class,
    DatasetDao.class,
    DatasetTableDao.class,
    AssetDao.class,
    DatasetRelationshipDao.class,
    StorageResourceDao.class,
    SnapshotDao.class,
    SnapshotService.class,
    SnapshotMapTableDao.class,
    SnapshotTableDao.class
  })
  static class HarnessApplication {
    @org.springframework.context.annotation.Bean(name = "daoObjectMapper")
    ObjectMapper daoObjectMapper() {
      return new ObjectMapper()
          .registerModule(new ParameterNamesModule())
          .registerModule(new Jdk8Module())
          .registerModule(new JavaTimeModule())
          .configure(MapperFeature.ACCEPT_CASE_INSENSITIVE_VALUES, true);
    }

    @org.springframework.context.annotation.Bean
    ApplicationConfiguration applicationConfiguration() {
      ApplicationConfiguration configuration = new ApplicationConfiguration();
      configuration.setDnsName("drs.anv0");
      configuration.setFirestoreFutureTimeoutSeconds(30);
      return configuration;
    }

    @org.springframework.context.annotation.Bean(
        name = "performanceThreadpool",
        destroyMethod = "shutdownNow")
    ExecutorService performanceThreadpool() {
      return Executors.newSingleThreadExecutor();
    }

    @org.springframework.context.annotation.Bean
    NamedParameterJdbcTemplate namedParameterJdbcTemplate(
        @Qualifier("embeddedDataSource") DataSource dataSource) {
      return new NamedParameterJdbcTemplate(dataSource);
    }

    @org.springframework.context.annotation.Bean
    JdbcTemplate jdbcTemplate(@Qualifier("embeddedDataSource") DataSource dataSource) {
      return new JdbcTemplate(dataSource);
    }

    @org.springframework.context.annotation.Bean
    DrsDao drsDao(NamedParameterJdbcTemplate jdbcTemplate, DrsIdService drsIdService) {
      return new DrsDao(jdbcTemplate, drsIdService);
    }

    @org.springframework.context.annotation.Bean(name = "drsResolutionThreadpool")
    AsyncTaskExecutor drsResolutionThreadpool() {
      return new SimpleAsyncTaskExecutor("drs-resolution-test-");
    }
  }
}
