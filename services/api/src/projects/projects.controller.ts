import { Body, Controller, Delete, Get, Param, ParseUUIDPipe, Post, Put, Query, Req, UploadedFile, UseGuards, UseInterceptors } from '@nestjs/common';
import { ApiConsumes, ApiHeader, ApiTags } from '@nestjs/swagger';
import { FileInterceptor } from '@nestjs/platform-express';
import { Request } from 'express';
import multer from 'multer';
import { randomUUID } from 'node:crypto';
import { mkdirSync } from 'node:fs';
import path from 'node:path';
import { PageQueryDto } from '../common/page-query.dto';
import { SessionGuard } from '../auth/session.guard';
import { ActionsService } from './actions.service';
import { CreateProjectDto } from './dto/create-project.dto';
import { PutEnvDto } from './dto/put-env.dto';
import { RedeployDto } from './dto/redeploy.dto';
import { DeploymentsService, MAX_UPLOAD_BYTES } from './deployments.service';
import { EnvService } from './env.service';
import { ProjectsService } from './projects.service';

const uploadStorage = multer.diskStorage({
  destination: (_req, _file, cb) => {
    const dir = path.join(process.env.UPLOADS_DIR ?? '/data/uploads', 'tmp');
    mkdirSync(dir, { recursive: true });
    cb(null, dir);
  },
  filename: (_req, _file, cb) => cb(null, `${randomUUID()}.part`),
});

@ApiTags('projects')
@Controller('projects')
@UseGuards(SessionGuard)
export class ProjectsController {
  constructor(
    private readonly projects: ProjectsService,
    private readonly deployments: DeploymentsService,
    private readonly actions: ActionsService,
    private readonly env: EnvService,
  ) {}

  @Post()
  @ApiHeader({ name: 'X-CSRF-Token', required: true, description: 'csrfToken from login or /auth/me' })
  create(@Body() dto: CreateProjectDto, @Req() req: Request) {
    return this.projects.create(req.session.uid!, dto);
  }

  @Post(':id/deployments')
  @ApiHeader({ name: 'X-CSRF-Token', required: true, description: 'csrfToken from login or /auth/me' })
  @ApiConsumes('multipart/form-data')
  @UseInterceptors(
    FileInterceptor('file', { storage: uploadStorage, limits: { fileSize: MAX_UPLOAD_BYTES } }),
  )
  upload(
    @Param('id', ParseUUIDPipe) id: string,
    @Req() req: Request,
    @UploadedFile() file?: Express.Multer.File,
  ) {
    return this.deployments.uploadZip(id, req.session.uid!, file);
  }

  @Post(':id/redeploy')
  @ApiHeader({ name: 'X-CSRF-Token', required: true, description: 'csrfToken from login or /auth/me' })
  redeploy(@Param('id', ParseUUIDPipe) id: string, @Req() req: Request, @Body() dto: RedeployDto) {
    return this.deployments.redeploy(id, req.session.uid!, dto.sourceId);
  }

  @Get()
  list(@Query() query: PageQueryDto, @Req() req: Request) {
    return this.projects.list(req.session.uid!, query);
  }

  @Get(':id/deployments')
  listDeployments(
    @Param('id', ParseUUIDPipe) id: string,
    @Query() query: PageQueryDto,
    @Req() req: Request,
  ) {
    return this.deployments.listDeployments(id, req.session.uid!, query);
  }

  @Get(':id/sources')
  listSources(@Param('id', ParseUUIDPipe) id: string, @Query() query: PageQueryDto, @Req() req: Request) {
    return this.deployments.listSources(id, req.session.uid!, query);
  }

  @Get(':id/actions')
  listActions(
    @Param('id', ParseUUIDPipe) id: string,
    @Query() query: PageQueryDto,
    @Req() req: Request,
  ) {
    return this.actions.list(id, req.session.uid!, query);
  }

  @Post(':id/stop')
  @ApiHeader({ name: 'X-CSRF-Token', required: true, description: 'csrfToken from login or /auth/me' })
  stop(@Param('id', ParseUUIDPipe) id: string, @Req() req: Request) {
    return this.actions.request(id, req.session.uid!, 'stop');
  }

  @Post(':id/start')
  @ApiHeader({ name: 'X-CSRF-Token', required: true, description: 'csrfToken from login or /auth/me' })
  start(@Param('id', ParseUUIDPipe) id: string, @Req() req: Request) {
    return this.actions.request(id, req.session.uid!, 'start');
  }

  @Post(':id/restart')
  @ApiHeader({ name: 'X-CSRF-Token', required: true, description: 'csrfToken from login or /auth/me' })
  restart(@Param('id', ParseUUIDPipe) id: string, @Req() req: Request) {
    return this.actions.request(id, req.session.uid!, 'restart');
  }

  @Get(':id/env')
  listEnv(@Param('id', ParseUUIDPipe) id: string, @Req() req: Request) {
    return this.env.list(id, req.session.uid!);
  }

  @Put(':id/env/:key')
  @ApiHeader({ name: 'X-CSRF-Token', required: true, description: 'csrfToken from login or /auth/me' })
  putEnv(
    @Param('id', ParseUUIDPipe) id: string,
    @Param('key') key: string,
    @Body() dto: PutEnvDto,
    @Req() req: Request,
  ) {
    return this.env.put(id, req.session.uid!, key, dto.value);
  }

  @Delete(':id/env/:key')
  @ApiHeader({ name: 'X-CSRF-Token', required: true, description: 'csrfToken from login or /auth/me' })
  deleteEnv(@Param('id', ParseUUIDPipe) id: string, @Param('key') key: string, @Req() req: Request) {
    return this.env.remove(id, req.session.uid!, key);
  }

  @Get(':id')
  get(@Param('id', ParseUUIDPipe) id: string, @Req() req: Request) {
    return this.projects.get(req.session.uid!, id);
  }

  @Get(':id/database')
  getDatabase(@Param('id', ParseUUIDPipe) id: string, @Req() req: Request) {
    return this.projects.getDatabase(req.session.uid!, id);
  }
}
