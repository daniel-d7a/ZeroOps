import { Body, Controller, Delete, Get, HttpCode, Param, ParseUUIDPipe, Post, Query, Req, UploadedFile, UseGuards, UseInterceptors } from '@nestjs/common';
import { ApiConsumes, ApiHeader, ApiTags } from '@nestjs/swagger';
import { FileInterceptor } from '@nestjs/platform-express';
import { Request } from 'express';
import multer from 'multer';
import { randomUUID } from 'node:crypto';
import { mkdirSync } from 'node:fs';
import path from 'node:path';
import { PageQueryDto } from '../common/page-query.dto';
import { SessionGuard } from '../auth/session.guard';
import { CreateProjectDto } from './dto/create-project.dto';
import { DeploymentsService, MAX_UPLOAD_BYTES } from './deployments.service';
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

  @Delete(':id/sources/:sourceId')
  @ApiHeader({ name: 'X-CSRF-Token', required: true, description: 'csrfToken from login or /auth/me' })
  @HttpCode(204)
  deleteSource(
    @Param('id', ParseUUIDPipe) id: string,
    @Param('sourceId', ParseUUIDPipe) sourceId: string,
    @Req() req: Request,
  ) {
    return this.deployments.deleteSource(id, req.session.uid!, sourceId);
  }

  @Get(':id')
  get(@Param('id', ParseUUIDPipe) id: string, @Req() req: Request) {
    return this.projects.get(req.session.uid!, id);
  }
}
